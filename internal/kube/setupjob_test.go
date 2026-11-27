package kube

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/christiannarbon/ois/internal/execx"
)

const (
	getJobsCM = "kubectl --context ois-demo -n storage get configmap ois-jobs -o json"
	getJob    = "kubectl --context ois-demo -n storage get job buckets -o json"
	delJob    = "kubectl --context ois-demo -n storage delete job/buckets --ignore-not-found --wait=true"
	applyCmd  = "kubectl --context ois-demo apply --server-side --field-manager=ois --force-conflicts -f -"
	logsCmd   = "kubectl --context ois-demo -n storage logs job/buckets --all-containers --tail=20"
)

var jobManifest = []byte(`{"kind":"Job","metadata":{"name":"buckets","labels":{"ois/job-kind":"setup"}}}`)

func jobHash() string {
	sum := sha256.Sum256(jobManifest)
	return hex.EncodeToString(sum[:])
}

const patchPrefix = `kubectl --context ois-demo -n storage patch configmap ois-jobs --type=merge -p {"data":{"buckets":`

func patchCmd(value string) string { return patchPrefix + value + `}}` }

func cmJSON(data map[string]string) execx.Result {
	b, _ := json.Marshal(map[string]any{"kind": "ConfigMap", "data": data})
	return execx.Result{Stdout: b}
}

// jobJSON returns a Job whose True conditions are given as "Type" or "Type:Reason".
func jobJSON(conds ...string) execx.Result {
	cs := []map[string]string{}
	for _, c := range conds {
		typ, reason, _ := strings.Cut(c, ":")
		cs = append(cs, map[string]string{"type": typ, "status": "True", "reason": reason, "message": "boom"})
	}
	b, _ := json.Marshal(map[string]any{"kind": "Job", "status": map[string]any{"conditions": cs}})
	return execx.Result{Stdout: b}
}

// seqRunner answers `get job buckets` from replies in order (the last one repeats) and
// everything else from the Fake.
type seqRunner struct {
	*execx.Fake
	replies []execx.Result
	errs    []error
	n       int
}

func (s *seqRunner) Run(ctx context.Context, c execx.Cmd) (execx.Result, error) {
	res, err := s.Fake.Run(ctx, c)
	if strings.Join(append([]string{c.Name}, c.Args...), " ") != getJob {
		return res, err
	}
	i := min(s.n, len(s.replies)-1)
	s.n++
	return s.replies[i], s.errs[i]
}

func runJob(t *testing.T, r execx.Runner, timeout time.Duration) error {
	t.Helper()
	return New(r, demo).RunSetupJob(context.Background(), "storage", "buckets", jobManifest, timeout)
}

func TestRunSetupJob(t *testing.T) {
	old := jobPoll
	jobPoll = time.Millisecond
	t.Cleanup(func() { jobPoll = old })

	hash := jobHash()
	recorded := cmJSON(map[string]string{"buckets": hash, "other": "x"})
	rerun := []string{delJob, applyCmd, getJob, patchCmd(`"` + hash + `"`)}

	cases := []struct {
		name    string
		cm      execx.Result
		cmErr   error
		job     []execx.Result // replies to `get job`, in order
		jobErrs []error
		want    []string
	}{
		{name: "unchanged and complete is a no-op", cm: recorded,
			job: []execx.Result{jobJSON("Complete")}, jobErrs: []error{nil},
			want: []string{getJobsCM, getJob}},
		{name: "changed input re-runs", cm: cmJSON(map[string]string{"buckets": "old"}),
			job: []execx.Result{jobJSON("Complete")}, jobErrs: []error{nil},
			want: append([]string{getJobsCM}, rerun...)},
		{name: "unchanged but job deleted re-runs", cm: recorded,
			job:     []execx.Result{{}, jobJSON("Complete")},
			jobErrs: []error{notFound("jobs.batch", "buckets"), nil},
			want:    append([]string{getJobsCM, getJob}, rerun...)},
		{name: "unchanged but failed re-runs", cm: recorded,
			job: []execx.Result{jobJSON("Failed:BackoffLimitExceeded"), jobJSON("Complete")}, jobErrs: []error{nil, nil},
			want: append([]string{getJobsCM, getJob}, rerun...)},
		{name: "waits while running", cm: cmJSON(nil),
			job: []execx.Result{jobJSON(), jobJSON(), jobJSON("Complete")}, jobErrs: []error{nil, nil, nil},
			want: []string{getJobsCM, delJob, applyCmd, getJob, getJob, getJob, patchCmd(`"` + hash + `"`)}},
		{name: "dry run runs every step", cm: execx.Result{},
			job: []execx.Result{{}}, jobErrs: []error{nil},
			want: append([]string{getJobsCM}, rerun...)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := (&execx.Fake{}).On(getJobsCM, c.cm, c.cmErr)
			r := &seqRunner{Fake: f, replies: c.job, errs: c.jobErrs}
			if err := runJob(t, r, time.Minute); err != nil {
				t.Fatal(err)
			}
			assertLines(t, f, c.want...)
		})
	}
}

func TestRunSetupJobCreatesConfigMap(t *testing.T) {
	f := (&execx.Fake{}).
		On(getJobsCM, execx.Result{}, notFound("configmaps", "ois-jobs")).
		On(getJob, jobJSON("Complete"), nil).
		On(patchPrefix, execx.Result{}, notFound("configmaps", "ois-jobs"))
	if err := runJob(t, f, time.Minute); err != nil {
		t.Fatal(err)
	}
	hash := jobHash()
	assertLines(t, f, getJobsCM, delJob, applyCmd, getJob, patchCmd(`"`+hash+`"`), applyCmd)

	var cm struct {
		Kind     string
		Metadata struct {
			Name, Namespace string
			Labels          map[string]string
		}
		Data map[string]string
	}
	if err := json.Unmarshal(stdin(t, f.Calls[5]), &cm); err != nil {
		t.Fatal(err)
	}
	if cm.Kind != "ConfigMap" || cm.Metadata.Name != "ois-jobs" || cm.Metadata.Namespace != "storage" ||
		cm.Metadata.Labels["app.kubernetes.io/managed-by"] != "ois" || len(cm.Data) != 1 || cm.Data["buckets"] != hash {
		t.Fatalf("got %+v", cm)
	}
	if b, _ := io.ReadAll(f.Calls[2].Stdin); string(b) != string(jobManifest) {
		t.Fatalf("job manifest on stdin: %q", b)
	}
}

func TestRunSetupJobFailures(t *testing.T) {
	old := jobPoll
	jobPoll = time.Millisecond
	t.Cleanup(func() { jobPoll = old })
	boom := errors.New("apply rejected")

	cases := []struct {
		name    string
		rules   map[string]execx.Result
		errs    map[string]error
		timeout time.Duration
		wantErr string
		want    []string
	}{
		{name: "failed job reports reason and logs at once",
			rules: map[string]execx.Result{
				getJob:  jobJSON("Failed:BackoffLimitExceeded"),
				logsCmd: {Stdout: []byte("make_bucket failed: warehouse\n")}},
			timeout: time.Hour,
			wantErr: "job storage/buckets failed (BackoffLimitExceeded: boom)\nlast log lines:\nmake_bucket failed: warehouse",
			want:    []string{getJobsCM, delJob, applyCmd, getJob, logsCmd}},
		{name: "timeout",
			rules:   map[string]execx.Result{getJob: jobJSON()},
			timeout: 20 * time.Millisecond,
			wantErr: "job storage/buckets did not complete within 20ms"},
		{name: "apply error stops before waiting",
			errs:    map[string]error{applyCmd: boom},
			timeout: time.Minute,
			wantErr: "apply rejected",
			want:    []string{getJobsCM, delJob, applyCmd}},
		{name: "configmap read error stops at once",
			errs:    map[string]error{getJobsCM: errors.New("forbidden")},
			timeout: time.Minute,
			wantErr: "forbidden",
			want:    []string{getJobsCM}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &execx.Fake{}
			for p, r := range c.rules {
				f.On(p, r, nil)
			}
			for p, e := range c.errs {
				f.On(p, execx.Result{}, e)
			}
			err := runJob(t, f, c.timeout)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err %v, want %q", err, c.wantErr)
			}
			for _, l := range f.CommandLines() {
				if strings.Contains(l, " patch ") {
					t.Fatalf("hash recorded after a failure: %s", l)
				}
			}
			if c.want != nil {
				assertLines(t, f, c.want...)
			}
		})
	}
}

func TestRunSetupJobStopsOnCancel(t *testing.T) {
	old := jobPoll
	jobPoll = time.Hour
	t.Cleanup(func() { jobPoll = old })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	f := (&execx.Fake{}).On(getJob, jobJSON(), nil)
	err := New(f, demo).RunSetupJob(ctx, "storage", "buckets", jobManifest, time.Hour)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err %v", err)
	}
}

func TestForgetSetupJob(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantErr bool
	}{
		{"removes the key", nil, false},
		{"missing configmap is fine", notFound("configmaps", "ois-jobs"), false},
		{"other errors surface", errors.New("forbidden"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := (&execx.Fake{}).On(patchPrefix, execx.Result{}, c.err)
			err := New(f, demo).ForgetSetupJob(context.Background(), "storage", "buckets")
			if (err != nil) != c.wantErr {
				t.Fatalf("err %v", err)
			}
			assertLines(t, f, patchCmd("null"))
		})
	}
}

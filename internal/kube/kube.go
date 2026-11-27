// Package kube runs kubectl against exactly one context, taken from a guarded
// cluster.Target. Components never build kubectl argument lists themselves.
package kube

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/christiannarbon/ois/internal/cluster"
	"github.com/christiannarbon/ois/internal/execx"
)

// Client runs kubectl against exactly one context.
type Client struct {
	Run     execx.Runner
	Context string
	Project string // labels namespaces and the setup-job ConfigMap
}

func New(r execx.Runner, t cluster.Target) *Client {
	return &Client{Run: r, Context: t.Context, Project: t.Project}
}

const (
	managedBy = "app.kubernetes.io/managed-by"
	partOf    = "app.kubernetes.io/part-of"
)

// kubectl builds a Cmd: kubectl --context <ctx> <args...>.
func (k *Client) kubectl(args ...string) execx.Cmd {
	return execx.Cmd{Name: "kubectl", Args: append([]string{"--context", k.Context}, args...)}
}

// inNamespace prefixes args with -n ns; an empty ns means a cluster-scoped call.
func inNamespace(ns string, args ...string) []string {
	if ns == "" {
		return args
	}
	return append([]string{"-n", ns}, args...)
}

func seconds(d time.Duration) string {
	return "--timeout=" + strconv.Itoa(int(max(d.Round(time.Second), time.Second).Seconds())) + "s"
}

// isNotFound reports whether a failed kubectl call said the object does not exist.
func isNotFound(res execx.Result, err error) bool {
	if bytes.Contains(res.Stderr, []byte("(NotFound)")) {
		return true
	}
	var ee *execx.ExitError
	return errors.As(err, &ee) && strings.Contains(ee.Stderr, "(NotFound)")
}

// Apply runs `kubectl apply --server-side --field-manager=ois --force-conflicts -f -`
// with manifest on stdin. secrets are redacted from logs. Server-side apply lets
// `ois plan` diff exactly what deploy would change.
func (k *Client) Apply(ctx context.Context, manifest []byte, secrets ...string) error {
	c := k.kubectl("apply", "--server-side", "--field-manager=ois", "--force-conflicts", "-f", "-")
	c.Stdin = bytes.NewReader(manifest)
	c.Secrets = secrets
	_, err := k.Run.Run(ctx, c)
	return err
}

// EnsureNamespace applies a Namespace labelled app.kubernetes.io/part-of=<project> and
// app.kubernetes.io/managed-by=ois.
func (k *Client) EnsureNamespace(ctx context.Context, name string) error {
	return k.applyObject(ctx, map[string]any{
		"apiVersion": "v1",
		"kind":       "Namespace",
		"metadata":   map[string]any{"name": name, "labels": k.labels(nil)},
	})
}

func (k *Client) labels(extra map[string]string) map[string]string {
	l := map[string]string{}
	for key, v := range extra {
		l[key] = v
	}
	l[managedBy] = "ois"
	if k.Project != "" {
		l[partOf] = k.Project
	}
	return l
}

func (k *Client) applyObject(ctx context.Context, obj map[string]any, secrets ...string) error {
	b, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	return k.Apply(ctx, b, secrets...)
}

// Wait runs `kubectl wait -n ns <resource> --for=<condition> --timeout=<t>`,
// e.g. Wait(ctx, "storage", "job/ois-storage-buckets", "condition=complete", 2*time.Minute).
func (k *Client) Wait(ctx context.Context, ns, resource, condition string, timeout time.Duration) error {
	_, err := k.Run.Run(ctx, k.kubectl(inNamespace(ns, "wait", resource, "--for="+condition, seconds(timeout))...))
	return err
}

// RolloutStatus runs `kubectl rollout status -n ns <kind/name> --timeout=<t>`.
func (k *Client) RolloutStatus(ctx context.Context, ns, resource string, timeout time.Duration) error {
	_, err := k.Run.Run(ctx, k.kubectl(inNamespace(ns, "rollout", "status", resource, seconds(timeout))...))
	return err
}

// Delete runs `kubectl delete -n ns <resource> --ignore-not-found --wait=true`.
func (k *Client) Delete(ctx context.Context, ns, resource string) error {
	_, err := k.Run.Run(ctx, k.kubectl(inNamespace(ns, "delete", resource, "--ignore-not-found", "--wait=true")...))
	return err
}

// GetJSON runs `kubectl get -n ns <args...> -o json` and returns stdout.
// NotFound, and empty output in dry run, return (nil, false, nil).
func (k *Client) GetJSON(ctx context.Context, ns string, args ...string) ([]byte, bool, error) {
	a := append(inNamespace(ns, "get"), args...)
	res, err := k.Run.Run(ctx, k.kubectl(append(a, "-o", "json")...))
	if isNotFound(res, err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	out := bytes.TrimSpace(res.Stdout)
	if len(out) == 0 {
		return nil, false, nil
	}
	return out, true, nil
}

// Exec runs `kubectl exec -n ns <pod> -c <container> [-i] [-t] -- cmd...` with Stream.
// -i is set when stdin is not nil.
func (k *Client) Exec(ctx context.Context, ns, pod, container string, cmd []string,
	stdin io.Reader, stdout, stderr io.Writer, tty bool) error {
	args := inNamespace(ns, "exec", pod, "-c", container)
	if stdin != nil {
		args = append(args, "-i")
	}
	if tty {
		args = append(args, "-t")
	}
	c := k.kubectl(append(append(args, "--"), cmd...)...)
	c.Stdin = stdin
	return k.Run.Stream(ctx, c, stdout, stderr)
}

// PortForward runs `kubectl port-forward -n ns <target> <local>:<remote>` until ctx is done.
// Cancelling ctx is the normal way to stop it and returns nil.
func (k *Client) PortForward(ctx context.Context, ns, target string, local, remote int,
	stdout, stderr io.Writer) error {
	c := k.kubectl(inNamespace(ns, "port-forward", target, fmt.Sprintf("%d:%d", local, remote))...)
	err := k.Run.Stream(ctx, c, stdout, stderr)
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// jobsConfigMap records, per namespace, the manifest hash of every setup Job that completed.
const jobsConfigMap = "ois-jobs"

// jobPoll is how often a running setup Job is checked.
var jobPoll = 2 * time.Second

// RunSetupJob runs a one-shot Job labelled ois/job-kind=setup only when its input changed:
// it hashes manifest (sha256), compares with key <name> of ConfigMap ois-jobs in ns, and
// if equal and the Job completed, does nothing. Otherwise it deletes the old Job, applies
// the new one, waits until it completes, and records the hash. Re-runs are no-ops.
// A Job that fails is reported as soon as it fails, with the end of its logs.
func (k *Client) RunSetupJob(ctx context.Context, ns, name string, manifest []byte, timeout time.Duration) error {
	sum := sha256.Sum256(manifest)
	hash := hex.EncodeToString(sum[:])
	recorded, err := k.recordedJobHash(ctx, ns, name)
	if err != nil {
		return err
	}
	if recorded == hash {
		st, err := k.jobState(ctx, ns, name)
		if err != nil {
			return err
		}
		if st.complete {
			return nil
		}
	}
	if err := k.Delete(ctx, ns, "job/"+name); err != nil {
		return err
	}
	if err := k.Apply(ctx, manifest); err != nil {
		return err
	}
	if err := k.waitJob(ctx, ns, name, timeout); err != nil {
		return err
	}
	return k.patchJobHash(ctx, ns, name, hash)
}

// ForgetSetupJob removes name's hash from ConfigMap ois-jobs, so the next RunSetupJob
// runs it even if unchanged (used when state was lost, and by a future --rerun-jobs flag).
func (k *Client) ForgetSetupJob(ctx context.Context, ns, name string) error {
	_, err := k.patchJobs(ctx, ns, name, nil)
	return err
}

func (k *Client) recordedJobHash(ctx context.Context, ns, name string) (string, error) {
	out, found, err := k.GetJSON(ctx, ns, "configmap", jobsConfigMap)
	if err != nil || !found {
		return "", err
	}
	var cm struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(out, &cm); err != nil {
		return "", fmt.Errorf("reading configmap %s/%s: %w", ns, jobsConfigMap, err)
	}
	return cm.Data[name], nil
}

// patchJobHash sets one key with a merge patch, so other Jobs' hashes are never touched,
// and creates the ConfigMap on first use.
func (k *Client) patchJobHash(ctx context.Context, ns, name, hash string) error {
	notFound, err := k.patchJobs(ctx, ns, name, hash)
	if !notFound {
		return err
	}
	return k.applyObject(ctx, map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": jobsConfigMap, "namespace": ns, "labels": k.labels(nil)},
		"data":       map[string]string{name: hash},
	})
}

// patchJobs merge-patches data.<name> to value (nil deletes the key). A missing
// ConfigMap is reported as notFound with no error.
func (k *Client) patchJobs(ctx context.Context, ns, name string, value any) (notFound bool, err error) {
	patch, err := json.Marshal(map[string]any{"data": map[string]any{name: value}})
	if err != nil {
		return false, err
	}
	res, err := k.Run.Run(ctx, k.kubectl(inNamespace(ns, "patch", "configmap", jobsConfigMap, "--type=merge", "-p", string(patch))...))
	if isNotFound(res, err) {
		return true, nil
	}
	return false, err
}

type jobState struct {
	exists           bool
	complete, failed bool
	reason           string
}

func (k *Client) jobState(ctx context.Context, ns, name string) (jobState, error) {
	out, found, err := k.GetJSON(ctx, ns, "job", name)
	if err != nil || !found {
		return jobState{}, err
	}
	return parseJobState(out)
}

func parseJobState(out []byte) (jobState, error) {
	var job struct {
		Status struct {
			Conditions []struct {
				Type, Status, Reason, Message string
			} `json:"conditions"`
		} `json:"status"`
	}
	if err := json.Unmarshal(out, &job); err != nil {
		return jobState{}, fmt.Errorf("reading job: %w", err)
	}
	st := jobState{exists: true}
	for _, c := range job.Status.Conditions {
		if c.Status != "True" {
			continue
		}
		switch c.Type {
		case "Complete":
			st.complete = true
		case "Failed":
			st.failed = true
			st.reason = strings.TrimSpace(c.Reason + ": " + c.Message)
		}
	}
	return st, nil
}

// waitJob polls the Job until it is Complete or Failed. `kubectl wait
// --for=condition=complete` alone would sit out the whole timeout on a failed Job.
// Empty output without an error (dry run) cannot be checked and counts as done.
func (k *Client) waitJob(ctx context.Context, ns, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		res, err := k.Run.Run(ctx, k.kubectl(inNamespace(ns, "get", "job", name, "-o", "json")...))
		if err != nil {
			return err
		}
		out := bytes.TrimSpace(res.Stdout)
		if len(out) == 0 {
			return nil
		}
		st, err := parseJobState(out)
		if err != nil {
			return err
		}
		switch {
		case st.complete:
			return nil
		case st.failed:
			return fmt.Errorf("job %s/%s failed (%s)%s", ns, name, st.reason, k.jobLogs(ctx, ns, name))
		case !time.Now().Before(deadline):
			return fmt.Errorf("job %s/%s did not complete within %s%s", ns, name, timeout, k.jobLogs(ctx, ns, name))
		}
		t := time.NewTimer(min(jobPoll, time.Until(deadline)))
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

func (k *Client) jobLogs(ctx context.Context, ns, name string) string {
	res, err := k.Run.Run(ctx, k.kubectl(inNamespace(ns, "logs", "job/"+name, "--all-containers", "--tail=20")...))
	logs := strings.TrimSpace(string(res.Stdout))
	if err != nil || logs == "" {
		return ""
	}
	return "\nlast log lines:\n" + logs
}

package cluster

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/christiannarbon/ois/internal/execx"
	"github.com/christiannarbon/ois/internal/spec"
)

var demo = Target{Project: "demo", Profile: "dev", Kind: spec.TargetMinikube, Context: "ois-demo"}

func TestWaitReady(t *testing.T) {
	f := &execx.Fake{}
	if err := WaitReady(context.Background(), f, demo, 300*time.Second); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"kubectl --context ois-demo wait --for=condition=Ready nodes --all --timeout=300s",
		"kubectl --context ois-demo wait --for=condition=Ready pods --all -n kube-system --field-selector=status.phase!=Succeeded --timeout=300s",
	}
	if got := f.CommandLines(); !slices.Equal(got, want) {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestWaitReadyStopsWhenNodesFail(t *testing.T) {
	boom := errors.New("timed out")
	f := (&execx.Fake{}).On("kubectl --context ois-demo wait --for=condition=Ready nodes", execx.Result{}, boom)
	if err := WaitReady(context.Background(), f, demo, time.Minute); !errors.Is(err, boom) {
		t.Fatalf("err %v", err)
	}
	if n := len(f.Calls); n != 1 {
		t.Fatalf("%d calls, want 1", n)
	}
}

func TestTimeoutFlagSharesOneBudget(t *testing.T) {
	if got := timeoutFlag(time.Now().Add(90 * time.Second)); got != "--timeout=90s" {
		t.Fatalf("got %s", got)
	}
	if got := timeoutFlag(time.Now().Add(-time.Minute)); got != "--timeout=1s" {
		t.Fatalf("spent budget: got %s", got)
	}
}

const scCmd = "kubectl --context ois-demo get storageclass -o json"

func TestRequireOneDefaultStorageClass(t *testing.T) {
	sc := func(name string, def bool) string {
		ann := ""
		if def {
			ann = `, "annotations": {"storageclass.kubernetes.io/is-default-class": "true"}`
		}
		return `{"metadata": {"name": "` + name + `"` + ann + `}}`
	}
	cases := []struct {
		name, out, wantErr string
	}{
		{"one default", `{"items": [` + sc("standard", true) + `, ` + sc("slow", false) + `]}`, ""},
		{"dry run", "", ""},
		{"two defaults", `{"items": [` + sc("standard", true) + `, ` + sc("fast", true) + `]}`,
			`cluster "ois-demo" must have exactly one default StorageClass, found 2: [standard, fast]`},
		{"none", `{"items": []}`, "found 0: []"},
		{"bad json", "{", "reading storage classes"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := (&execx.Fake{}).On(scCmd, execx.Result{Stdout: []byte(c.out)}, nil)
			err := RequireOneDefaultStorageClass(context.Background(), f, demo)
			if c.wantErr == "" && err != nil || c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
				t.Fatalf("err %v, want %q", err, c.wantErr)
			}
		})
	}
}

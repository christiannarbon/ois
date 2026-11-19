package cluster

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	assets "github.com/christiannarbon/ois/components"
	"github.com/christiannarbon/ois/internal/execx"
	"github.com/christiannarbon/ois/internal/project"
)

// Addons are enabled on every profile; they provide the default StorageClass.
var Addons = []string{"default-storageclass", "storage-provisioner"}

// State is whether a profile exists and runs.
type State string

const (
	StateMissing State = "missing"
	StateRunning State = "running"
	StateStopped State = "stopped"
	StateInvalid State = "invalid" // listed by minikube as invalid; only delete helps
)

// DriverEnv overrides the minikube driver.
const DriverEnv = "OIS_MINIKUBE_DRIVER"

const defaultDriver = "docker"

// Manager runs minikube for one profile.
type Manager struct {
	Run        execx.Runner
	Profile    string // ois-<project>
	K8sVersion string
	Driver     string
	Progress   io.Writer // optional: stream minikube start output here
}

// New builds a Manager for a project, reading the pinned Kubernetes version.
func New(r execx.Runner, projectName string) (*Manager, error) {
	ver, err := assets.Version("KUBERNETES_VERSION")
	if err != nil {
		return nil, err
	}
	return &Manager{
		Run:        r,
		Profile:    project.ProfileName(projectName),
		K8sVersion: ver,
		Driver:     cmp.Or(os.Getenv(DriverEnv), defaultDriver),
	}, nil
}

// Status is what minikube reports about the profile. Size, K8sVersion and Driver are
// set when the profile exists and is valid; Size.Disk is always empty (not reported).
type Status struct {
	State      State
	Size       Size
	K8sVersion string
	Driver     string
}

type profileList struct {
	Invalid []profileEntry `json:"invalid"`
	Valid   []profileEntry `json:"valid"`
}

type profileEntry struct {
	Name   string `json:"Name"`
	Status string `json:"Status"`
	Config struct {
		CPUs             int    `json:"CPUs"`
		Memory           int    `json:"Memory"`
		Driver           string `json:"Driver"`
		KubernetesConfig struct {
			KubernetesVersion string `json:"KubernetesVersion"`
		} `json:"KubernetesConfig"`
	} `json:"Config"`
}

const noProfiles = "No minikube profile was found"

// runningStatuses are the profile statuses that mean the cluster is up. Newer minikube
// reports a healthy running profile as "OK"; anything else is treated as stopped, so
// Start brings it back.
var runningStatuses = []string{"Running", "OK", "Degraded"}

// Inspect returns what minikube reports about the profile.
func (m *Manager) Inspect(ctx context.Context) (Status, error) {
	res, err := m.minikube(ctx, "profile", "list", "-o", "json")
	if err != nil {
		var ee *execx.ExitError
		if errors.As(err, &ee) && strings.Contains(ee.Stderr, noProfiles) {
			return Status{State: StateMissing}, nil
		}
		return Status{}, err
	}
	out := bytes.TrimSpace(res.Stdout)
	if len(out) == 0 {
		return Status{State: StateMissing}, nil
	}
	var list profileList
	if err := json.Unmarshal(out, &list); err != nil {
		return Status{}, fmt.Errorf("reading minikube profile list: %w", err)
	}
	for _, p := range list.Invalid {
		if p.Name == m.Profile {
			return Status{State: StateInvalid}, nil
		}
	}
	for _, p := range list.Valid {
		if p.Name != m.Profile {
			continue
		}
		st := Status{
			State:      StateStopped,
			Size:       Size{CPUs: p.Config.CPUs, MemoryMiB: p.Config.Memory},
			K8sVersion: p.Config.KubernetesConfig.KubernetesVersion,
			Driver:     p.Config.Driver,
		}
		if slices.Contains(runningStatuses, p.Status) {
			st.State = StateRunning
		}
		return st, nil
	}
	return Status{State: StateMissing}, nil
}

// InvalidError explains how to get rid of a profile minikube reports as invalid.
func (m *Manager) InvalidError() error {
	return fmt.Errorf("minikube reports cluster %s as invalid; delete it with: ois down --purge", m.Profile)
}

// Start creates or starts the profile. It never changes the current kube context.
// With Progress set, minikube's output is streamed there; otherwise it is captured and
// its tail is added to the error on failure.
func (m *Manager) Start(ctx context.Context, want Size) error {
	c := execx.Cmd{Name: "minikube", Args: []string{"start", "-p", m.Profile,
		"--driver=" + m.Driver,
		"--kubernetes-version=" + m.K8sVersion,
		"--cpus=" + strconv.Itoa(want.CPUs),
		"--memory=" + strconv.Itoa(want.MemoryMiB),
		"--disk-size=" + want.Disk,
		"--keep-context",
		"--interactive=false",
	}}
	if m.Progress != nil {
		return m.Run.Stream(ctx, c, m.Progress, m.Progress)
	}
	res, err := m.Run.Run(ctx, c)
	if err != nil {
		if tail := lastLines(res.Stdout, startOutputLines); tail != "" {
			return fmt.Errorf("%w\nminikube output (last %d lines):\n%s", err, startOutputLines, tail)
		}
	}
	return err
}

const startOutputLines = 15

func lastLines(b []byte, n int) string {
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// EnableAddons enables Addons on the profile.
func (m *Manager) EnableAddons(ctx context.Context) error {
	for _, addon := range Addons {
		if _, err := m.minikube(ctx, "addons", "enable", addon, "-p", m.Profile); err != nil {
			return err
		}
	}
	return nil
}

// Stop stops a running profile and returns the state it found. Stopped or missing
// profiles are left alone; an invalid one is an error.
func (m *Manager) Stop(ctx context.Context) (State, error) {
	st, err := m.Inspect(ctx)
	switch {
	case err != nil:
		return "", err
	case st.State == StateInvalid:
		return st.State, m.InvalidError()
	case st.State != StateRunning:
		return st.State, nil
	}
	_, err = m.minikube(ctx, "stop", "-p", m.Profile)
	return st.State, err
}

// Delete deletes the profile if it exists, including an invalid one. Missing: no-op.
func (m *Manager) Delete(ctx context.Context) error {
	st, err := m.Inspect(ctx)
	if err != nil || st.State == StateMissing {
		return err
	}
	return m.Purge(ctx)
}

// Purge deletes the profile without checking it first and removes its kubeconfig
// entries. Callers that already ran Inspect use it to avoid a second profile list.
func (m *Manager) Purge(ctx context.Context) error {
	if _, err := m.minikube(ctx, "delete", "-p", m.Profile); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"config", "delete-context", m.Profile},
		{"config", "delete-cluster", m.Profile},
		{"config", "unset", "users." + m.Profile},
	} {
		// minikube delete usually removes these already; leftovers are harmless.
		_, _ = m.Run.Run(ctx, execx.Cmd{Name: "kubectl", Args: args})
	}
	return nil
}

func (m *Manager) minikube(ctx context.Context, args ...string) (execx.Result, error) {
	return m.Run.Run(ctx, execx.Cmd{Name: "minikube", Args: args})
}

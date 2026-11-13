package cluster

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

type profileList struct {
	Invalid []profileEntry `json:"invalid"`
	Valid   []profileEntry `json:"valid"`
}

type profileEntry struct {
	Name   string `json:"Name"`
	Status string `json:"Status"`
	Config struct {
		CPUs   int `json:"CPUs"`
		Memory int `json:"Memory"`
	} `json:"Config"`
}

const noProfiles = "No minikube profile was found"

// runningStatuses are the profile statuses that mean the cluster is up. Newer minikube
// reports a healthy running profile as "OK"; anything else is treated as stopped, so
// Start brings it back.
var runningStatuses = []string{"Running", "OK", "Degraded"}

// Inspect returns the profile state and, when it exists, its live size (Disk is empty —
// minikube does not report it).
func (m *Manager) Inspect(ctx context.Context) (State, Size, error) {
	res, err := m.minikube(ctx, "profile", "list", "-o", "json")
	if err != nil {
		var ee *execx.ExitError
		if errors.As(err, &ee) && strings.Contains(ee.Stderr, noProfiles) {
			return StateMissing, Size{}, nil
		}
		return "", Size{}, err
	}
	out := bytes.TrimSpace(res.Stdout)
	if len(out) == 0 {
		return StateMissing, Size{}, nil
	}
	var list profileList
	if err := json.Unmarshal(out, &list); err != nil {
		return "", Size{}, fmt.Errorf("reading minikube profile list: %w", err)
	}
	for _, p := range list.Invalid {
		if p.Name == m.Profile {
			return "", Size{}, fmt.Errorf("minikube reports profile %s as invalid; delete it with: minikube delete -p %s", m.Profile, m.Profile)
		}
	}
	for _, p := range list.Valid {
		if p.Name != m.Profile {
			continue
		}
		size := Size{CPUs: p.Config.CPUs, MemoryMiB: p.Config.Memory}
		if slices.Contains(runningStatuses, p.Status) {
			return StateRunning, size, nil
		}
		return StateStopped, size, nil
	}
	return StateMissing, Size{}, nil
}

// Start creates or starts the profile. It never changes the current kube context.
func (m *Manager) Start(ctx context.Context, want Size) error {
	_, err := m.minikube(ctx, "start", "-p", m.Profile,
		"--driver="+m.Driver,
		"--kubernetes-version="+m.K8sVersion,
		"--cpus="+strconv.Itoa(want.CPUs),
		"--memory="+strconv.Itoa(want.MemoryMiB),
		"--disk-size="+want.Disk,
		"--keep-context",
		"--interactive=false",
	)
	return err
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

// Stop stops a running profile. Missing profile: no-op.
func (m *Manager) Stop(ctx context.Context) error {
	state, _, err := m.Inspect(ctx)
	if err != nil || state != StateRunning {
		return err
	}
	_, err = m.minikube(ctx, "stop", "-p", m.Profile)
	return err
}

// Delete deletes the profile and removes its kubeconfig entries. Missing profile: no-op.
func (m *Manager) Delete(ctx context.Context) error {
	state, _, err := m.Inspect(ctx)
	if err != nil || state == StateMissing {
		return err
	}
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

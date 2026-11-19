package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/christiannarbon/ois/internal/execx"
)

const defaultClassAnnotation = "storageclass.kubernetes.io/is-default-class"

func kubectl(t Target, args ...string) execx.Cmd {
	return execx.Cmd{Name: "kubectl", Args: append([]string{"--context", t.Context}, args...)}
}

// WaitReady waits until every node and every kube-system pod is Ready, all within timeout.
// Node Ready alone is not enough: right after a start or resume the control plane pods
// can still be restarting.
func WaitReady(ctx context.Context, r execx.Runner, t Target, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	if _, err := r.Run(ctx, kubectl(t, "wait", "--for=condition=Ready", "nodes", "--all", timeoutFlag(deadline))); err != nil {
		return err
	}
	_, err := r.Run(ctx, kubectl(t, "wait", "--for=condition=Ready", "pods", "--all", "-n", "kube-system",
		"--field-selector=status.phase!=Succeeded", timeoutFlag(deadline)))
	return err
}

func timeoutFlag(deadline time.Time) string {
	left := max(time.Until(deadline).Round(time.Second), time.Second)
	return fmt.Sprintf("--timeout=%ds", int(left.Seconds()))
}

// RequireOneDefaultStorageClass fails unless exactly one StorageClass is the default.
// Empty output (dry run) cannot be checked and passes.
func RequireOneDefaultStorageClass(ctx context.Context, r execx.Runner, t Target) error {
	res, err := r.Run(ctx, kubectl(t, "get", "storageclass", "-o", "json"))
	if err != nil {
		return err
	}
	out := bytes.TrimSpace(res.Stdout)
	if len(out) == 0 {
		return nil
	}
	var list struct {
		Items []struct {
			Metadata struct {
				Name        string            `json:"name"`
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return fmt.Errorf("reading storage classes: %w", err)
	}
	var defaults []string
	for _, sc := range list.Items {
		if sc.Metadata.Annotations[defaultClassAnnotation] == "true" {
			defaults = append(defaults, sc.Metadata.Name)
		}
	}
	if len(defaults) != 1 {
		return fmt.Errorf("cluster %q must have exactly one default StorageClass, found %d: [%s]",
			t.Context, len(defaults), strings.Join(defaults, ", "))
	}
	return nil
}

// Package cluster manages the minikube profile that backs one project.
package cluster

import (
	"fmt"

	"github.com/christiannarbon/ois/internal/spec"
)

// Size is the resources of a minikube profile.
type Size struct {
	CPUs      int
	MemoryMiB int
	Disk      string // "30g"
}

// SizeFor maps cluster.size to concrete resources (minimal 2/4096/20g, lean 4/7168/30g,
// custom from the spec). The spec is assumed validated.
func SizeFor(c spec.Cluster) Size {
	switch c.Size {
	case "minimal":
		return Size{CPUs: 2, MemoryMiB: 4096, Disk: "20g"}
	case "custom":
		return Size{CPUs: c.CPUs, MemoryMiB: c.Memory, Disk: c.Disk}
	default:
		return Size{CPUs: 4, MemoryMiB: 7168, Disk: "30g"}
	}
}

func (s Size) String() string {
	out := fmt.Sprintf("%d CPU, %d MiB", s.CPUs, s.MemoryMiB)
	if s.Disk != "" {
		out += ", " + s.Disk
	}
	return out
}

package cluster

import (
	"testing"

	"github.com/christiannarbon/ois/internal/spec"
)

func TestSizeFor(t *testing.T) {
	cases := []struct {
		in   spec.Cluster
		want Size
		str  string
	}{
		{spec.Cluster{Size: "minimal"}, Size{2, 4096, "20g"}, "2 CPU, 4096 MiB, 20g"},
		{spec.Cluster{Size: "lean"}, Size{4, 7168, "30g"}, "4 CPU, 7168 MiB, 30g"},
		{spec.Cluster{Size: "custom", CPUs: 6, Memory: 12288, Disk: "60g"}, Size{6, 12288, "60g"}, "6 CPU, 12288 MiB, 60g"},
	}
	for _, c := range cases {
		t.Run(c.in.Size, func(t *testing.T) {
			got := SizeFor(c.in)
			if got != c.want {
				t.Fatalf("SizeFor = %+v, want %+v", got, c.want)
			}
			if got.String() != c.str {
				t.Fatalf("String = %q, want %q", got.String(), c.str)
			}
		})
	}
}

func TestSizeStringWithoutDisk(t *testing.T) {
	if got := (Size{CPUs: 4, MemoryMiB: 7168}).String(); got != "4 CPU, 7168 MiB" {
		t.Fatalf("got %q", got)
	}
}

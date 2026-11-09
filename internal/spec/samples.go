package spec

import (
	"fmt"
	"slices"
	"sync"
)

var (
	samplesMu sync.Mutex
	samples   = map[string]bool{}
)

// RegisterSample declares a sample name valid in `samples:`. Called from init() by the
// package that ships the sample. Empty or duplicate names panic.
func RegisterSample(name string) {
	samplesMu.Lock()
	defer samplesMu.Unlock()
	if name == "" || samples[name] {
		panic(fmt.Sprintf("spec: bad or duplicate sample %q", name))
	}
	samples[name] = true
}

// KnownSamples returns registered names, sorted.
func KnownSamples() []string {
	samplesMu.Lock()
	defer samplesMu.Unlock()
	out := make([]string, 0, len(samples))
	for name := range samples {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

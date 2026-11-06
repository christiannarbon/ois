package execx

import (
	"sort"
	"strings"
)

// Redact replaces every non-empty secret in s with "***", longest first so a secret that
// contains another is never half-revealed.
func Redact(s string, secrets []string) string {
	if len(secrets) == 0 {
		return s
	}
	sorted := make([]string, 0, len(secrets))
	for _, sec := range secrets {
		if sec != "" {
			sorted = append(sorted, sec)
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	for _, sec := range sorted {
		s = strings.ReplaceAll(s, sec, "***")
	}
	return s
}

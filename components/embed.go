// Package components embeds the per-component deployment assets and pinned versions.
// Import it as: assets "github.com/christiannarbon/ois/components"
package components

import "embed"

// FS holds every embedded asset. Add each new component directory to this line.
//
//go:embed versions.env storage
var FS embed.FS

// ReadFile reads an embedded asset, e.g. ReadFile("storage/values.yaml").
func ReadFile(path string) ([]byte, error) { return FS.ReadFile(path) }

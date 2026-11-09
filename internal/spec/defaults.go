package spec

// applyDefaults fills in values the user may leave out. Component blocks default
// inside their own components, not here.
func applyDefaults(s *Spec) {
	if s.Cluster.Size == "" {
		s.Cluster.Size = "lean"
	}
}

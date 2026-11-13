package spec

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	TargetMinikube   = "minikube"
	TargetKubernetes = "kubernetes"
	TierDev          = "dev"
	TierProduction   = "production"
	SecretsGenerate  = "generate"
	SecretsExisting  = "existing"
	DefaultProfile   = "dev"
)

// Profile is one entry under `profiles:` as written by the user.
type Profile struct {
	Target          string         `yaml:"target,omitempty"`
	Context         string         `yaml:"context,omitempty"`
	Tier            string         `yaml:"tier,omitempty"`
	NamespacePrefix string         `yaml:"namespace_prefix,omitempty"`
	Secrets         string         `yaml:"secrets,omitempty"`
	Images          *ProfileImages `yaml:"images,omitempty"`
	// Components holds per-profile overrides exactly as written; Resolve merges them.
	Components yaml.Node `yaml:"components,omitempty"`
}

type ProfileImages struct {
	Registry   string `yaml:"registry,omitempty"`    // e.g. ghcr.io/me — no scheme
	PullSecret string `yaml:"pull_secret,omitempty"` // existing imagePullSecret name
}

// External holds bring-your-own services for one profile. Nil = installed by ois.
type External struct {
	Storage *ExternalStorage
	Kafka   *ExternalKafka
	Catalog *ExternalCatalog
}

type ExternalStorage struct {
	Endpoint          string            `yaml:"endpoint"`
	Region            string            `yaml:"region,omitempty"`
	PathStyle         bool              `yaml:"path_style,omitempty"`
	CredentialsSecret string            `yaml:"credentials_secret"` // keys: access-key, secret-key
	Buckets           map[string]string `yaml:"buckets"`            // keys: warehouse, flink
}

type ExternalKafka struct {
	Bootstrap         string `yaml:"bootstrap"`
	Security          string `yaml:"security,omitempty"`           // PLAINTEXT (default) | SASL_SSL
	CredentialsSecret string `yaml:"credentials_secret,omitempty"` // keys: username, password; required for SASL_SSL
}

type ExternalCatalog struct {
	URI                string            `yaml:"uri"`
	Warehouse          string            `yaml:"warehouse"`
	CredentialsSecrets map[string]string `yaml:"credentials_secrets"` // engine -> Secret (keys: client-id, client-secret)
}

// ResolvedProfile is a profile with every default applied.
type ResolvedProfile struct {
	Name            string // "dev", "prod", ...
	Target          string // TargetMinikube | TargetKubernetes
	Context         string // minikube: "ois-<project>"; kubernetes: profiles.<p>.context
	Tier            string // TierDev | TierProduction
	NamespacePrefix string
	Secrets         string // SecretsGenerate | SecretsExisting
	ImageRegistry   string
	ImagePullSecret string
	External        External
}

// Namespace returns the namespace for a base name ("storage") under this profile.
func (p ResolvedProfile) Namespace(base string) string { return p.NamespacePrefix + base }

// IsExternal reports whether a component is provided outside ois for this profile.
func (p ResolvedProfile) IsExternal(component string) bool {
	switch component {
	case "storage":
		return p.External.Storage != nil
	case "kafka":
		return p.External.Kafka != nil
	case "catalog":
		return p.External.Catalog != nil
	}
	return false
}

// Resolved is the spec as one profile sees it. Its embedded *Spec is a copy that shares
// nothing with the loaded spec: Components are the merged view, Samples are cleared on
// kubernetes targets, Profiles are empty and Resolve on it is an error.
// Everything downstream (registry, Render, Deploy) uses Resolved, never Spec.Profiles.
type Resolved struct {
	*Spec
	Profile ResolvedProfile
}

// ProfileNames returns "dev" plus every declared profile, sorted, without duplicates.
func (s *Spec) ProfileNames() []string {
	names := []string{DefaultProfile}
	for name := range s.Profiles {
		if name != DefaultProfile {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

var externalComponents = []string{"storage", "kafka", "catalog"}

// Resolve returns the view of the spec for one profile. Unknown names are an error:
//
//	unknown profile "prd" (available: dev, prod)
//
// Merge/decode problems are returned as *ValidationError with lines inside
// profiles.<name>.components.
func (s *Spec) Resolve(name string) (*Resolved, error) {
	if s.view != "" {
		return nil, fmt.Errorf("spec is already resolved for profile %q", s.view)
	}
	p, declared := s.Profiles[name]
	if !declared && name != DefaultProfile {
		return nil, fmt.Errorf("unknown profile %q (available: %s)", name, strings.Join(s.ProfileNames(), ", "))
	}
	rp := resolveProfile(name, p, s.Project)
	prefix := "profiles." + name + ".components"

	base, err := expandAliases(s.componentsNode)
	if err != nil {
		return nil, s.validationError([]FieldError{expandFieldError("components", err)})
	}
	if base != nil && base.Kind != yaml.MappingNode {
		base = nil
	}
	nullsToMappings(base)

	var errs []FieldError
	override, err := expandAliases(&p.Components)
	switch {
	case err != nil:
		errs = append(errs, expandFieldError(prefix, err))
		override = nil
	case override.Kind == 0, override.Kind == yaml.ScalarNode && override.Tag == "!!null":
		override = nil
	case override.Kind != yaml.MappingNode:
		errs = append(errs, FieldError{Path: prefix, Msg: "expected a mapping", Line: p.Components.Line})
		override = nil
	}
	nullsToMappings(override)

	merged := mergeNodes(base, override)
	external, externalLines := liftExternal(merged)
	var comps Components
	if merged != nil {
		// Top-level keys were checked by Parse, so an unknown key here is in the profile.
		errs = append(errs, s.decodeStrict(merged, &comps, prefix)...)
	}
	for _, comp := range slices.Sorted(maps.Keys(external)) {
		path := prefix + "." + comp + ".external"
		node, line := external[comp], externalLines[comp]
		switch {
		case !slices.Contains(externalComponents, comp):
			errs = append(errs, FieldError{Path: path, Msg: "external is only supported for storage, kafka and catalog", Line: line})
		case rp.Target != TargetKubernetes:
			errs = append(errs, FieldError{Path: path, Msg: "external services need target: kubernetes", Line: line})
		default:
			errs = append(errs, s.decodeExternal(&rp.External, comp, node, path)...)
		}
	}
	if len(errs) > 0 {
		return nil, s.validationError(errs)
	}

	cp := s.viewCopy(name)
	cp.Components = comps
	if rp.Target == TargetKubernetes {
		cp.Samples = nil
	}
	return &Resolved{Spec: cp, Profile: rp}, nil
}

// defaultTarget is minikube for dev and kubernetes for every other profile: dev is the
// only profile with a local cluster, so no two profiles can share one.
func defaultTarget(name string) string {
	if name == DefaultProfile {
		return TargetMinikube
	}
	return TargetKubernetes
}

func resolveProfile(name string, p Profile, project string) ResolvedProfile {
	rp := ResolvedProfile{
		Name:            name,
		Target:          cmp.Or(p.Target, defaultTarget(name)),
		Tier:            p.Tier,
		NamespacePrefix: p.NamespacePrefix,
		Secrets:         p.Secrets,
	}
	if p.Images != nil {
		rp.ImageRegistry, rp.ImagePullSecret = p.Images.Registry, p.Images.PullSecret
	}
	if rp.Target == TargetKubernetes {
		rp.Context = p.Context
		rp.Tier = cmp.Or(rp.Tier, TierProduction)
		rp.Secrets = cmp.Or(rp.Secrets, SecretsExisting)
	} else {
		// Must match project.ContextName; spec cannot import internal/project.
		rp.Context = "ois-" + project
		rp.Tier = cmp.Or(rp.Tier, TierDev)
		rp.Secrets = cmp.Or(rp.Secrets, SecretsGenerate)
	}
	return rp
}

func (s *Spec) decodeExternal(ext *External, comp string, node *yaml.Node, path string) []FieldError {
	switch comp {
	case "storage":
		ext.Storage = &ExternalStorage{}
		return s.decodeStrict(node, ext.Storage, path)
	case "kafka":
		ext.Kafka = &ExternalKafka{}
		errs := s.decodeStrict(node, ext.Kafka, path)
		ext.Kafka.Security = cmp.Or(ext.Kafka.Security, "PLAINTEXT")
		return errs
	case "catalog":
		ext.Catalog = &ExternalCatalog{}
		return s.decodeStrict(node, ext.Catalog, path)
	}
	return nil
}

// validationError fills missing lines from the nearest recorded parent and sorts.
func (s *Spec) validationError(errs []FieldError) *ValidationError {
	for i := range errs {
		if errs[i].Line == 0 {
			errs[i].Line = s.nearestLine(errs[i].Path)
		}
	}
	sortFieldErrors(errs)
	return &ValidationError{File: s.File, Errors: errs}
}

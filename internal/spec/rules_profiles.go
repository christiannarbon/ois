package spec

import (
	"cmp"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

var (
	profileNameRe     = regexp.MustCompile(`^[a-z][a-z0-9-]{0,19}$`)
	namespacePrefixRe = regexp.MustCompile(`^([a-z0-9][a-z0-9-]*)?$`)
)

// longestBaseNamespace is len("orchestration"); prefix + base must fit a 63-char label.
const longestBaseNamespace = 13

// catalogEngines are the components that talk to an external catalog with their own credentials.
var catalogEngines = []string{"flink", "trino", "dagster"}

// externalBuckets are the buckets an external storage must name.
var externalBuckets = []string{"warehouse", "flink"}

// checkProfile checks one declared profile's own settings. Component overrides and
// external placement are checked by Resolve.
func checkProfile(s *Spec, name string, p Profile) []FieldError {
	path := "profiles." + name
	var errs []FieldError
	add := func(field, msg string) {
		fp := path
		if field != "" {
			fp += "." + field
		}
		errs = append(errs, FieldError{Path: fp, Msg: msg})
	}
	if !profileNameRe.MatchString(name) {
		add("", "profile names must match "+profileNameRe.String())
	}
	target := cmp.Or(p.Target, defaultTarget(name))
	switch {
	case name == DefaultProfile && target == TargetKubernetes:
		add("target", "dev always runs on minikube; put a real cluster in a profile of its own")
	case name != DefaultProfile && target == TargetMinikube:
		add("target", "only dev runs on minikube; other profiles need target: kubernetes")
	}
	switch target {
	case TargetMinikube:
		if p.Context != "" {
			add("context", "context is always ois-<project> for target: minikube; remove it")
		}
	case TargetKubernetes:
		if p.Context == "" {
			add("context", "context is required for target: kubernetes (ois never uses your current context)")
		}
	default:
		add("target", "target must be minikube or kubernetes")
	}
	if !slices.Contains([]string{"", TierDev, TierProduction}, p.Tier) {
		add("tier", "tier must be dev or production")
	}
	if !slices.Contains([]string{"", SecretsGenerate, SecretsExisting}, p.Secrets) {
		add("secrets", "secrets must be generate or existing")
	}
	if !namespacePrefixRe.MatchString(p.NamespacePrefix) || len(p.NamespacePrefix)+longestBaseNamespace > 63 {
		add("namespace_prefix", fmt.Sprintf("namespace_prefix must be a DNS label prefix of at most %d characters", 63-longestBaseNamespace))
	}
	if p.Images != nil && strings.Contains(p.Images.Registry, "://") {
		add("images.registry", "registry is a host/path like ghcr.io/me, without a scheme")
	}
	return errs
}

// checkExternal checks the required fields of each external service in a resolved profile.
func checkExternal(r *Resolved) []FieldError {
	path := "profiles." + r.Profile.Name + ".components."
	var errs []FieldError
	required := func(p, field, value string) {
		if value == "" {
			errs = append(errs, FieldError{Path: p + "." + field, Msg: lastSegment(field) + " is required"})
		}
	}
	ext := r.Profile.External
	if st := ext.Storage; st != nil {
		p := path + "storage.external"
		required(p, "endpoint", st.Endpoint)
		required(p, "credentials_secret", st.CredentialsSecret)
		for _, b := range externalBuckets {
			required(p, "buckets."+b, st.Buckets[b])
		}
		errs = append(errs, unknownMapKeys(p+".buckets", "bucket", st.Buckets, externalBuckets)...)
	}
	if k := ext.Kafka; k != nil {
		p := path + "kafka.external"
		required(p, "bootstrap", k.Bootstrap)
		switch k.Security {
		case "PLAINTEXT":
		case "SASL_SSL":
			if k.CredentialsSecret == "" {
				errs = append(errs, FieldError{Path: p + ".credentials_secret", Msg: "credentials_secret is required for security: SASL_SSL"})
			}
		default:
			errs = append(errs, FieldError{Path: p + ".security", Msg: "security must be PLAINTEXT or SASL_SSL"})
		}
	}
	if c := ext.Catalog; c != nil {
		p := path + "catalog.external"
		required(p, "uri", c.URI)
		required(p, "warehouse", c.Warehouse)
		for _, engine := range catalogEngines {
			if r.Enabled(engine) && c.CredentialsSecrets[engine] == "" {
				errs = append(errs, FieldError{
					Path: p + ".credentials_secrets." + engine,
					Msg:  fmt.Sprintf("credentials_secrets.%s is required because %s is enabled", engine, engine),
				})
			}
		}
		errs = append(errs, unknownMapKeys(p+".credentials_secrets", "engine", c.CredentialsSecrets, catalogEngines)...)
	}
	return errs
}

// unknownMapKeys reports every key of m that is not in known, at path.<key>.
func unknownMapKeys(path, what string, m map[string]string, known []string) []FieldError {
	var errs []FieldError
	for _, k := range slices.Sorted(maps.Keys(m)) {
		if slices.Contains(known, k) {
			continue
		}
		msg := fmt.Sprintf("unknown %s (known: %s)", what, strings.Join(known, ", "))
		if s := suggest(k, known); s != "" {
			msg = fmt.Sprintf("unknown %s (did you mean %q?)", what, s)
		}
		errs = append(errs, FieldError{Path: path + "." + k, Msg: msg})
	}
	return errs
}

func lastSegment(path string) string {
	return path[strings.LastIndexByte(path, '.')+1:]
}

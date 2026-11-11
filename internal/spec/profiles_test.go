package spec

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func mustParseFixture(t *testing.T, name string) *Spec {
	t.Helper()
	s, err := parseFixture(t, name)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustResolve(t *testing.T, s *Spec, name string) *Resolved {
	t.Helper()
	r, err := s.Resolve(name)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// validationErrors returns the errors of a *ValidationError, failing the test otherwise.
func validationErrors(t *testing.T, err error) []FieldError {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v (%T), want *ValidationError", err, err)
	}
	return ve.Errors
}

func findError(t *testing.T, errs []FieldError, path, msg string) FieldError {
	t.Helper()
	for _, fe := range errs {
		if fe.Path == path && strings.Contains(fe.Msg, msg) {
			return fe
		}
	}
	t.Fatalf("no error at %s containing %q in %+v", path, msg, errs)
	return FieldError{}
}

func TestResolveNoProfiles(t *testing.T) {
	s := mustParseFixture(t, "profiles-none.yaml")
	if got := s.ProfileNames(); !slices.Equal(got, []string{"dev"}) {
		t.Fatalf("ProfileNames = %v", got)
	}
	r := mustResolve(t, s, "dev")
	want := ResolvedProfile{Name: "dev", Target: TargetMinikube, Context: "ois-demo", Tier: TierDev, Secrets: SecretsGenerate}
	if r.Profile != want {
		t.Fatalf("profile = %+v, want %+v", r.Profile, want)
	}
	if !r.Enabled("storage") || !slices.Equal(r.Samples, []string{"test-sample"}) {
		t.Fatalf("view: storage %v samples %v", r.Enabled("storage"), r.Samples)
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestResolveUnknownProfile(t *testing.T) {
	s, err := Parse([]byte(header+"components: {storage: {}}\nprofiles:\n  prod: {target: kubernetes, context: c}\n"), "ois.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Resolve("prd")
	if err == nil || err.Error() != `unknown profile "prd" (available: dev, prod)` {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveKubernetesDefaults(t *testing.T) {
	s := mustParseFixture(t, "profiles-prod.yaml")
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := s.ProfileNames(); !slices.Equal(got, []string{"dev", "prod", "staging"}) {
		t.Fatalf("ProfileNames = %v", got)
	}
	prod := mustResolve(t, s, "prod")
	want := ResolvedProfile{
		Name: "prod", Target: TargetKubernetes, Context: "prod-admin", Tier: TierProduction,
		NamespacePrefix: "lake-", Secrets: SecretsExisting, ImageRegistry: "ghcr.io/me", ImagePullSecret: "ghcr-pull",
	}
	if prod.Profile != want {
		t.Fatalf("prod = %+v, want %+v", prod.Profile, want)
	}
	if prod.Samples != nil {
		t.Fatalf("samples not cleared on kubernetes: %v", prod.Samples)
	}
	staging := mustResolve(t, s, "staging")
	if staging.Profile.Tier != TierDev || staging.Profile.Secrets != SecretsGenerate {
		t.Fatalf("declared tier/secrets not kept: %+v", staging.Profile)
	}
}

func TestResolveDoesNotChangeSpec(t *testing.T) {
	s := mustParseFixture(t, "profiles-prod.yaml")
	mustResolve(t, s, "prod")
	if s.Enabled("trino") || s.Samples == nil {
		t.Fatal("Resolve changed the original spec")
	}
	dev := mustResolve(t, s, "dev")
	if dev.Enabled("trino") || !slices.Equal(dev.Samples, []string{"test-sample"}) {
		t.Fatalf("dev view: trino %v samples %v", dev.Enabled("trino"), dev.Samples)
	}
}

func TestResolveOverrideMerge(t *testing.T) {
	s := mustParseFixture(t, "profiles-prod.yaml")
	prod := mustResolve(t, s, "prod")
	for _, name := range []string{"storage", "kafka", "trino"} {
		if !prod.Enabled(name) {
			t.Errorf("prod: %s not enabled", name)
		}
	}
	if prod.Enabled("catalog") {
		t.Error("prod: catalog enabled but never listed")
	}
	if mustResolve(t, s, "dev").Enabled("trino") {
		t.Error("dev: trino enabled; only prod adds it")
	}
}

func TestResolveOverrideUnknownKey(t *testing.T) {
	s := mustParseFixture(t, "profiles-bad-override.yaml")
	_, err := s.Resolve("prod")
	fe := findError(t, validationErrors(t, err), "profiles.prod.components.kafka.replicas", "unknown field")
	if fe.Line != 12 {
		t.Fatalf("line = %d, want 12 (inside the profile)", fe.Line)
	}
	errs := validationErrors(t, s.Validate())
	findError(t, errs, "profiles.prod.components.kafka.replicas", "unknown field")
}

func TestResolveOverrideTypo(t *testing.T) {
	s, err := Parse([]byte(header+"components: {storage: {}}\nprofiles:\n  prod:\n    components:\n      trnio: {}\n"), "ois.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Resolve("prod")
	fe := findError(t, validationErrors(t, err), "profiles.prod.components.trnio", `did you mean "trino"?`)
	if fe.Line != 7 {
		t.Fatalf("line = %d, want 7", fe.Line)
	}
}

func TestResolveOverrideBadType(t *testing.T) {
	s, err := Parse([]byte(header+"components: {storage: {}}\nprofiles:\n  prod:\n    components:\n      kafka: [x]\n"), "ois.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Resolve("prod")
	fe := findError(t, validationErrors(t, err), "profiles.prod.components.kafka", "expected a mapping")
	if fe.Line != 7 {
		t.Fatalf("line = %d, want 7", fe.Line)
	}
}

func TestResolveExternal(t *testing.T) {
	s := mustParseFixture(t, "profiles-external.yaml")
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	prod := mustResolve(t, s, "prod")
	ext := prod.Profile.External
	if ext.Storage == nil || ext.Storage.Buckets["flink"] != "lake-flink" || !ext.Storage.PathStyle {
		t.Fatalf("storage external = %+v", ext.Storage)
	}
	if ext.Kafka == nil || ext.Kafka.Security != "SASL_SSL" || ext.Catalog == nil || ext.Catalog.CredentialsSecrets["trino"] != "trino-catalog" {
		t.Fatalf("external = %+v / %+v", ext.Kafka, ext.Catalog)
	}
	for _, c := range []string{"storage", "kafka", "catalog"} {
		if !prod.Profile.IsExternal(c) || !prod.Enabled(c) {
			t.Errorf("%s: external %v enabled %v", c, prod.Profile.IsExternal(c), prod.Enabled(c))
		}
	}
	if prod.Profile.IsExternal("trino") || mustResolve(t, s, "dev").Profile.IsExternal("storage") {
		t.Error("IsExternal true where nothing is external")
	}
	again := mustResolve(t, s, "prod")
	if again.Profile.External.Storage == nil {
		t.Fatal("second Resolve lost the external block; the profile node was changed")
	}
}

func TestResolveKafkaSecurityDefault(t *testing.T) {
	yml := header + "components: {storage: {}, kafka: {}}\nprofiles:\n  prod:\n    target: kubernetes\n    context: c\n    components:\n      kafka:\n        external: {bootstrap: k:9092}\n"
	s, err := Parse([]byte(yml), "ois.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if got := mustResolve(t, s, "prod").Profile.External.Kafka.Security; got != "PLAINTEXT" {
		t.Fatalf("security = %q, want PLAINTEXT", got)
	}
}

func TestProfileExternalRules(t *testing.T) {
	k8s := "    target: kubernetes\n    context: c\n"
	base := header + "components: {storage: {}, kafka: {}, catalog: {}, flink: {}, trino: {}, dagster: {}}\nprofiles:\n  prod:\n"
	tests := []struct {
		name, profile, path, msg string
	}{
		{"under flink", k8s + "    components:\n      flink:\n        external: {}\n",
			"profiles.prod.components.flink.external", "external is only supported for storage, kafka and catalog"},
		{"on minikube", "    components:\n      storage:\n        external: {endpoint: e}\n",
			"profiles.prod.components.storage.external", "external services need target: kubernetes"},
		{"storage missing flink bucket", k8s + "    components:\n      storage:\n        external:\n          endpoint: e\n          credentials_secret: s\n          buckets: {warehouse: w}\n",
			"profiles.prod.components.storage.external.buckets.flink", "flink is required"},
		{"storage missing endpoint", k8s + "    components:\n      storage:\n        external: {credentials_secret: s, buckets: {warehouse: w, flink: f}}\n",
			"profiles.prod.components.storage.external.endpoint", "endpoint is required"},
		{"storage unknown key", k8s + "    components:\n      storage:\n        external: {endpiont: e}\n",
			"profiles.prod.components.storage.external.endpiont", `did you mean "endpoint"?`},
		{"kafka bootstrap", k8s + "    components:\n      kafka:\n        external: {security: PLAINTEXT}\n",
			"profiles.prod.components.kafka.external.bootstrap", "bootstrap is required"},
		{"kafka security", k8s + "    components:\n      kafka:\n        external: {bootstrap: b, security: SSL}\n",
			"profiles.prod.components.kafka.external.security", "security must be PLAINTEXT or SASL_SSL"},
		{"kafka sasl secret", k8s + "    components:\n      kafka:\n        external: {bootstrap: b, security: SASL_SSL}\n",
			"profiles.prod.components.kafka.external.credentials_secret", "required for security: SASL_SSL"},
		{"catalog engine secret", k8s + "    components:\n      catalog:\n        external: {uri: u, warehouse: w, credentials_secrets: {flink: f, trino: t}}\n",
			"profiles.prod.components.catalog.external.credentials_secrets.dagster", "credentials_secrets.dagster is required because dagster is enabled"},
		{"catalog uri", k8s + "    components:\n      catalog:\n        external: {warehouse: w, credentials_secrets: {flink: f, trino: t, dagster: d}}\n",
			"profiles.prod.components.catalog.external.uri", "uri is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Parse([]byte(base+tt.profile), "ois.yaml")
			if err != nil {
				t.Fatal(err)
			}
			fe := findError(t, validationErrors(t, s.Validate()), tt.path, tt.msg)
			if fe.Line <= 0 {
				t.Fatalf("line = %d", fe.Line)
			}
		})
	}
}

func TestProfileFieldRules(t *testing.T) {
	tests := []struct {
		name, profiles, path, msg string
	}{
		{"name", "  Prod_1: {}\n", "profiles.Prod_1", "profile names must match"},
		{"target", "  prod: {target: gke}\n", "profiles.prod.target", "target must be minikube or kubernetes"},
		{"kubernetes context", "  prod: {target: kubernetes}\n", "profiles.prod.context", "context is required for target: kubernetes"},
		{"minikube context", "  prod: {context: my-ctx}\n", "profiles.prod.context", "context is always ois-<project> for target: minikube; remove it"},
		{"dev context", "  dev: {context: my-ctx}\n", "profiles.dev.context", "remove it"},
		{"tier", "  prod: {tier: staging}\n", "profiles.prod.tier", "tier must be dev or production"},
		{"secrets", "  prod: {secrets: vault}\n", "profiles.prod.secrets", "secrets must be generate or existing"},
		{"prefix pattern", "  prod: {namespace_prefix: Lake_}\n", "profiles.prod.namespace_prefix", "at most 50 characters"},
		{"prefix length", "  prod: {namespace_prefix: " + strings.Repeat("a", 51) + "}\n", "profiles.prod.namespace_prefix", "DNS label prefix"},
		{"registry scheme", "  prod: {images: {registry: \"https://ghcr.io/me\"}}\n", "profiles.prod.images.registry", "without a scheme"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := Parse([]byte(header+"components: {storage: {}}\nprofiles:\n"+tt.profiles), "ois.yaml")
			if err != nil {
				t.Fatal(err)
			}
			fe := findError(t, validationErrors(t, s.Validate()), tt.path, tt.msg)
			if fe.Line <= 0 {
				t.Fatalf("line = %d", fe.Line)
			}
		})
	}
	s, err := Parse([]byte(header+"components: {storage: {}}\nprofiles:\n  prod: {namespace_prefix: "+strings.Repeat("a", 50)+"}\n"), "ois.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("50-character prefix rejected: %v", err)
	}
}

func TestProfileNamespace(t *testing.T) {
	r := mustResolve(t, mustParseFixture(t, "profiles-prod.yaml"), "prod")
	if got := r.Profile.Namespace("storage"); got != "lake-storage" {
		t.Fatalf("Namespace = %q", got)
	}
	if got := (ResolvedProfile{}).Namespace("storage"); got != "storage" {
		t.Fatalf("Namespace without prefix = %q", got)
	}
}

func TestProfileTopLevelErrorReportedOnce(t *testing.T) {
	yml := "apiVersion: ois/v1alpha1\nproject: Bad_Name\ncomponents: {storage: {}}\nprofiles:\n" +
		"  prod: {target: kubernetes, context: p}\n  staging: {target: kubernetes, context: s}\n  qa: {}\n"
	s, err := Parse([]byte(yml), "ois.yaml")
	if err != nil {
		t.Fatal(err)
	}
	errs := validationErrors(t, s.Validate())
	if len(errs) != 1 || errs[0].Path != "project" {
		t.Fatalf("errors = %+v, want one project error", errs)
	}
}

func TestProfileFixedByProfileNotReported(t *testing.T) {
	yml := header + "components: {storage: {}, trino: {}}\ntransform: {}\nprofiles:\n  prod:\n    components:\n      dagster: {}\n"
	s, err := Parse([]byte(yml), "ois.yaml")
	if err != nil {
		t.Fatal(err)
	}
	errs := validationErrors(t, s.Validate())
	// dev lacks dagster; prod adds it, so the error belongs to the top level (and dev) only.
	findError(t, errs, "transform", "needs components.dagster to be enabled")
	for _, fe := range errs {
		if strings.Contains(fe.Msg, "(profile") {
			t.Fatalf("unexpected profile-only error %+v", fe)
		}
	}

	yml = header + "components: {storage: {}, kafka: {}, catalog: {}, flink: {}}\nstreams:\n  - name: a\nprofiles:\n  prod:\n    components:\n      superset: {}\ndashboards: {}\n"
	s, err = Parse([]byte(yml), "ois.yaml")
	if err != nil {
		t.Fatal(err)
	}
	errs = validationErrors(t, s.Validate())
	findError(t, errs, "dashboards", "needs components.superset to be enabled")
	for _, fe := range errs {
		if strings.Contains(fe.Msg, "(profile prod)") {
			t.Fatalf("prod enables superset, so it must not report: %+v", fe)
		}
	}
}

// profileProbeProject makes the test-only rule below fire; no real spec uses this name.
const profileProbeProject = "profile-probe"

func init() {
	RegisterRule(Rule{Name: "zz-test-profile-probe", Check: func(s *Spec) []FieldError {
		if s.Project != profileProbeProject || !s.Enabled("trino") {
			return nil
		}
		return []FieldError{{Path: "components", Msg: "probe saw trino"}}
	}})
}

func TestProfileOnlyProblemIsLabelled(t *testing.T) {
	yml := "apiVersion: ois/v1alpha1\nproject: " + profileProbeProject + "\ncomponents: {storage: {}}\nprofiles:\n" +
		"  prod:\n    components:\n      trino: {}\n  qa:\n    components:\n      trino: {}\n"
	s, err := Parse([]byte(yml), "ois.yaml")
	if err != nil {
		t.Fatal(err)
	}
	errs := validationErrors(t, s.Validate())
	var msgs []string
	for _, fe := range errs {
		msgs = append(msgs, fe.Path+": "+fe.Msg)
	}
	want := []string{"components: probe saw trino (profile prod)", "components: probe saw trino (profile qa)"}
	if !slices.Equal(msgs, want) {
		t.Fatalf("errors = %q, want %q", msgs, want)
	}
}

func TestProfileNullComponentsInProfile(t *testing.T) {
	yml := header + "components: {storage: {}}\nprofiles:\n  prod:\n    components:\n      trino:\n  qa:\n    components:\n"
	s, err := Parse([]byte(yml), "ois.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !mustResolve(t, s, "prod").Enabled("trino") {
		t.Fatal("bare trino: in a profile did not enable it")
	}
	if qa := mustResolve(t, s, "qa"); !qa.Enabled("storage") {
		t.Fatal("empty profile components: disabled the top-level components")
	}
}

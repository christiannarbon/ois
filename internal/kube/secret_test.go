package kube

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"testing"

	"github.com/christiannarbon/ois/internal/execx"
)

func TestGetSecret(t *testing.T) {
	const cmd = "kubectl --context ois-demo -n storage get secret creds -o json"
	f := (&execx.Fake{}).On(cmd, execx.Result{Stdout: []byte(
		`{"kind":"Secret","data":{"access-key":"YWRtaW4=","secret-key":"cDRzcyB3MHJk"}}`)}, nil)
	data, found, err := newClient(f).GetSecret(context.Background(), "storage", "creds")
	if err != nil || !found {
		t.Fatalf("found %v, err %v", found, err)
	}
	if want := map[string]string{"access-key": "admin", "secret-key": "p4ss w0rd"}; !maps.Equal(data, want) {
		t.Fatalf("got %v", data)
	}
	assertLines(t, f, cmd)
}

func TestGetSecretNotFoundAndErrors(t *testing.T) {
	const cmd = "kubectl --context ois-demo -n storage get secret creds -o json"
	cases := []struct {
		name    string
		res     execx.Result
		err     error
		wantErr string
	}{
		{"not found", execx.Result{}, notFound("secrets", "creds"), ""},
		{"dry run", execx.Result{}, nil, ""},
		{"bad base64", execx.Result{Stdout: []byte(`{"data":{"k":"%%%"}}`)}, nil, "key k"},
		{"bad json", execx.Result{Stdout: []byte(`{`)}, nil, "reading secret storage/creds"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := (&execx.Fake{}).On(cmd, c.res, c.err)
			data, found, err := newClient(f).GetSecret(context.Background(), "storage", "creds")
			if found || data != nil {
				t.Fatalf("found %v, data %v", found, data)
			}
			if c.wantErr == "" && err != nil || c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
				t.Fatalf("err %v, want %q", err, c.wantErr)
			}
		})
	}
}

func TestApplySecretKeepsValuesOffArgv(t *testing.T) {
	f := (&execx.Fake{}).On("kubectl", execx.Result{}, errors.New("rejected: hunter2-secret"))
	data := map[string]string{"access-key": "admin-key-1", "secret-key": "hunter2-secret"}
	err := newClient(f).ApplySecret(context.Background(), "storage", "creds", data,
		map[string]string{"app.kubernetes.io/component": "storage", "app.kubernetes.io/managed-by": "someone-else"})
	if err == nil {
		t.Fatal("want the runner's error")
	}

	c := f.Calls[0]
	line := f.CommandLines()[0]
	if line != "kubectl --context ois-demo apply --server-side --field-manager=ois --force-conflicts -f -" {
		t.Fatalf("line %s", line)
	}
	for _, v := range data {
		if strings.Contains(line, v) || strings.Contains(c.String(), v) {
			t.Fatalf("value %q on the command line", v)
		}
		if !strings.Contains(strings.Join(c.Secrets, " "), v) {
			t.Fatalf("value %q not marked secret: %q", v, c.Secrets)
		}
	}

	var s struct {
		APIVersion, Kind, Type string
		Metadata               struct {
			Name, Namespace string
			Labels          map[string]string
		}
		StringData map[string]string `json:"stringData"`
		Data       map[string]string `json:"data"`
	}
	if err := json.Unmarshal(stdin(t, c), &s); err != nil {
		t.Fatal(err)
	}
	if s.APIVersion != "v1" || s.Kind != "Secret" || s.Type != "Opaque" ||
		s.Metadata.Name != "creds" || s.Metadata.Namespace != "storage" || s.Data != nil {
		t.Fatalf("got %+v", s)
	}
	if !maps.Equal(s.StringData, data) {
		t.Fatalf("stringData %v", s.StringData)
	}
	wantLabels := map[string]string{"app.kubernetes.io/component": "storage", "app.kubernetes.io/managed-by": "ois"}
	if !maps.Equal(s.Metadata.Labels, wantLabels) {
		t.Fatalf("labels %v", s.Metadata.Labels)
	}
}

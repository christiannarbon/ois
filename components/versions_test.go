package components

import (
	"strings"
	"testing"
)

func TestParseEnv(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    map[string]string
		wantErr string
	}{
		{
			name: "quoted, bare, comments and blanks",
			in:   "# header\n\nA=\"1.0\"\nB='2.0'\n  # indented comment\nC=3.0\n",
			want: map[string]string{"A": "1.0", "B": "2.0", "C": "3.0"},
		},
		{name: "empty values", in: "A=\nB=\"\"\nC=''\n", want: map[string]string{"A": "", "B": "", "C": ""}},
		{name: "url value", in: `R="https://charts.example.com"`, want: map[string]string{"R": "https://charts.example.com"}},
		{name: "crlf", in: "A=\"1\"\r\nB=2\r\n", want: map[string]string{"A": "1", "B": "2"}},
		{name: "empty file", in: "", want: map[string]string{}},
		{name: "malformed line", in: "A=1\n\nFOO\n", wantErr: "line 3"},
		{name: "duplicate key", in: "A=1\nA=2\n", wantErr: "line 2: duplicate key A"},
		{name: "latest", in: "A=\"1\"\nB=\"Latest\"\n", wantErr: "line 2"},
		{name: "bare latest", in: "B=latest\n", wantErr: "latest"},
		{name: "export", in: "export A=1\n", wantErr: "line 1"},
		{name: "lowercase key", in: "a=1\n", wantErr: "line 1"},
		{name: "expansion", in: "A=\"${HOME}\"\n", wantErr: "line 1"},
		{name: "command substitution", in: "A=$(date)\n", wantErr: "line 1"},
		{name: "unterminated quote", in: "A=\"1.0\n", wantErr: "line 1"},
		{name: "trailing text", in: "A=\"1.0\" # note\n", wantErr: "line 1"},
		{name: "bare with space", in: "A=1 2\n", wantErr: "line 1"},
		{name: "lone quote", in: "A=\"\n", wantErr: "line 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseEnv([]byte(tt.in))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for k, v := range tt.want {
				if g, ok := got[k]; !ok || g != v {
					t.Errorf("%s = %q (present %v), want %q", k, g, ok, v)
				}
			}
		})
	}
}

func TestVersionsEmbedded(t *testing.T) {
	v, err := Versions()
	if err != nil {
		t.Fatal(err)
	}
	if v["KUBERNETES_VERSION"] == "" {
		t.Fatalf("KUBERNETES_VERSION missing or empty in %v", v)
	}
}

func TestVersionsReturnsCopy(t *testing.T) {
	v, err := Versions()
	if err != nil {
		t.Fatal(err)
	}
	v["KUBERNETES_VERSION"] = "changed"
	got, err := Version("KUBERNETES_VERSION")
	if err != nil {
		t.Fatal(err)
	}
	if got == "changed" {
		t.Fatal("mutating the returned map changed the cached versions")
	}
}

func TestVersion(t *testing.T) {
	if _, err := Version("KUBERNETES_VERSION"); err != nil {
		t.Fatal(err)
	}
	_, err := Version("NO_SUCH_KEY")
	if err == nil || !strings.Contains(err.Error(), "NO_SUCH_KEY") || !strings.Contains(err.Error(), "versions.env") {
		t.Fatalf("err = %v, want one naming the key and the file", err)
	}
}

func TestVersionNotPinned(t *testing.T) {
	v, err := Versions()
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range v {
		if value != "" {
			continue
		}
		if _, err := Version(key); err == nil || !strings.Contains(err.Error(), "not pinned") {
			t.Fatalf("Version(%s) err = %v, want a not-pinned error", key, err)
		}
		return
	}
	t.Skip("every key is pinned")
}

func TestReadFile(t *testing.T) {
	data, err := ReadFile("storage/values.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("storage/values.yaml is empty")
	}
}

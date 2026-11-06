package project

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func touch(t *testing.T, path string) {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte("name: demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// realTempDir resolves symlinks so paths compare equal on macOS, where /var is /private/var.
func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func noSpecAbove(t *testing.T, dir string) {
	t.Helper()
	if _, err := Find(dir); !errors.Is(err, ErrNotFound) {
		t.Skipf("an ois.yaml exists above %s; cannot test the not-found case here", dir)
	}
}

func TestFindWalksUp(t *testing.T) {
	root := realTempDir(t)
	want := filepath.Join(root, "a", FileName)
	touch(t, want)
	start := filepath.Join(root, "a", "b", "c")
	mkdir(t, start)

	got, err := Find(start)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Find = %q, want %q", got, want)
	}
}

func TestFindStartsAtStart(t *testing.T) {
	root := realTempDir(t)
	touch(t, filepath.Join(root, FileName))
	want := filepath.Join(root, "a", FileName)
	touch(t, want)

	got, err := Find(filepath.Join(root, "a"))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Find = %q, want the nearest %q", got, want)
	}
}

func TestFindSkipsDirectoryNamedLikeSpec(t *testing.T) {
	root := realTempDir(t)
	want := filepath.Join(root, FileName)
	touch(t, want)
	mkdir(t, filepath.Join(root, "a", FileName))

	got, err := Find(filepath.Join(root, "a"))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Find = %q, want %q", got, want)
	}
}

func TestFindNotFound(t *testing.T) {
	root := realTempDir(t)
	noSpecAbove(t, root)
	start := filepath.Join(root, "a", "b")
	mkdir(t, start)

	if _, err := Find(start); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Find error = %v, want ErrNotFound", err)
	}
}

func TestResolveRelativeFlagFile(t *testing.T) {
	dir := realTempDir(t)
	want := filepath.Join(dir, "custom.yaml")
	touch(t, want)

	got, err := Resolve("custom.yaml", dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Resolve = %q, want %q", got, want)
	}
}

func TestResolveAbsoluteFlagFile(t *testing.T) {
	dir := realTempDir(t)
	want := filepath.Join(dir, "custom.yaml")
	touch(t, want)

	got, err := Resolve(want, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Resolve = %q, want %q", got, want)
	}
}

func TestResolveMissingFlagFile(t *testing.T) {
	_, err := Resolve("custom.yaml", realTempDir(t))
	if err == nil || !strings.Contains(err.Error(), "custom.yaml") {
		t.Fatalf("Resolve error = %v, want one naming custom.yaml", err)
	}
}

func TestResolveDirectory(t *testing.T) {
	dir := realTempDir(t)
	want := filepath.Join(dir, FileName)
	touch(t, want)

	got, err := Resolve(dir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Resolve = %q, want %q", got, want)
	}
}

func TestResolveDirectoryWithoutSpec(t *testing.T) {
	dir := realTempDir(t)
	_, err := Resolve(dir, dir)
	if err == nil || !strings.Contains(err.Error(), filepath.Join(dir, FileName)) {
		t.Fatalf("Resolve error = %v, want one naming %s", err, filepath.Join(dir, FileName))
	}
}

func TestResolveFallsBackToFind(t *testing.T) {
	root := realTempDir(t)
	want := filepath.Join(root, FileName)
	touch(t, want)
	cwd := filepath.Join(root, "sub")
	mkdir(t, cwd)

	got, err := Resolve("", cwd)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Resolve = %q, want %q", got, want)
	}
}

func TestNames(t *testing.T) {
	if got := ProfileName("demo"); got != "ois-demo" {
		t.Errorf("ProfileName = %q, want ois-demo", got)
	}
	if got := ContextName("demo"); got != ProfileName("demo") {
		t.Errorf("ContextName = %q, want %q", got, ProfileName("demo"))
	}
}

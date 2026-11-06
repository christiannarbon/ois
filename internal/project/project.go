// Package project locates a project's ois.yaml and derives the names ois uses for it.
package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// FileName is the project spec file name.
const FileName = "ois.yaml"

// ErrNotFound is returned when no ois.yaml exists in start or any parent directory.
var ErrNotFound = errors.New("no ois.yaml found in this directory or any parent — run \"ois init\" or pass -f")

// Find walks up from start (inclusive) to the filesystem root and returns the
// absolute path of the first ois.yaml it finds.
func Find(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, FileName)
		info, err := os.Stat(candidate)
		switch {
		case err == nil && info.Mode().IsRegular():
			return candidate, nil
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			return "", fmt.Errorf("checking %s: %w", candidate, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNotFound
		}
		dir = parent
	}
}

// Resolve returns the absolute spec path: flagFile if non-empty (it must exist and be a
// regular file), otherwise Find(cwd). A relative flagFile is resolved against cwd, and a
// directory resolves to the ois.yaml inside it.
func Resolve(flagFile, cwd string) (string, error) {
	if flagFile == "" {
		return Find(cwd)
	}
	path := flagFile
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", specError(flagFile, err)
	}
	if info.IsDir() {
		path = filepath.Join(path, FileName)
		if info, err = os.Stat(path); err != nil {
			return "", specError(path, err)
		}
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("spec file %s is not a regular file", path)
	}
	return path, nil
}

func specError(path string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("spec file %s does not exist", path)
	}
	return fmt.Errorf("spec file %s: %w", path, err)
}

// ProfileName is the minikube profile for a project. It is also the kube context name.
func ProfileName(project string) string { return "ois-" + project }

// ContextName is the kube context ois uses for a project.
func ContextName(project string) string { return ProfileName(project) }

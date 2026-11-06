package execx

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"testing"
)

func TestOnlyExecxImportsOsExec(t *testing.T) {
	root := filepath.Join("..", "..")
	self, _ := filepath.Abs(".")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "my_notes", "testdata", "node_modules":
				return filepath.SkipDir
			}
			if abs, _ := filepath.Abs(path); abs == self {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == "os/exec" {
				t.Errorf("%s imports os/exec; run commands through internal/execx", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

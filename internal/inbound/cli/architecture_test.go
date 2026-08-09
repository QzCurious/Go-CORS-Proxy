package cli

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const inboundImportPrefix = "github.com/QzCurious/seamless-cors/internal/inbound/"

func TestOnlyCompositionRootsImportInboundAdapters(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate architecture test")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))

	err := filepath.WalkDir(repositoryRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		relative, err := filepath.Rel(repositoryRoot, path)
		if err != nil {
			return err
		}
		if relative == "cmd" || strings.HasPrefix(relative, "cmd"+string(filepath.Separator)) {
			return nil
		}

		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range parsed.Imports {
			importPath, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if strings.HasPrefix(importPath, inboundImportPrefix) {
				t.Errorf("production package outside cmd imports an Inbound Adapter: %s imports %s", relative, importPath)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

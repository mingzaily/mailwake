package auth

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"testing"
)

func TestProductionHashCostAPI(t *testing.T) {
	// Inspect the production file set even when this test binary uses mailwake_test.
	output, err := exec.Command("go", "list", "-json", "-tags=nomsgpack", ".").Output()
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct{ GoFiles []string }
	if err := json.Unmarshal(output, &pkg); err != nil {
		t.Fatal(err)
	}
	for _, name := range pkg.GoFiles {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.GenDecl:
				if decl.Tok != token.VAR {
					continue
				}
				for _, spec := range decl.Specs {
					for _, name := range spec.(*ast.ValueSpec).Names {
						if ast.IsExported(name.Name) {
							t.Errorf("mutable global exposed in production: %s", name.Name)
						}
					}
				}
			case *ast.FuncDecl:
				if decl.Name.Name == "UseTestPasswordHashParameters" {
					t.Error("test hash helper included in production")
				}
			}
		}
	}
}

package siren

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// TestCodesListsEveryConstant: Codes names each string constant of siren.go whose name starts with Code, and no other.
func TestCodesListsEveryConstant(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "siren.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, spec := range g.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, n := range vs.Names {
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !strings.HasPrefix(n.Name, "Code") || !ok || lit.Kind != token.STRING {
					continue
				}
				v, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				want = append(want, v)
			}
		}
	}
	got := slices.Clone(Codes())
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("Codes() = %v, the constants are %v", got, want)
	}
}

package view

import (
	"go/build"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheViewImportsNoStore: the view draws what it is given and reads no store. Of the packages of this module, the
// view and the packages that it imports, at any depth, import only siren and text: core and db are never among them.
// go/build reads the import lines of the files that a build for one system takes; the tests are not among them. The
// walk is done for linux and for windows, because a file for one system only (a name that ends in _windows.go, a
// build line) is not in the build for the other.
func TestTheViewImportsNoStore(t *testing.T) {
	const module, view = "lifelog/", "lifelog/internal/api/view"
	root, err := filepath.Abs(filepath.Join("..", "..", "..")) // the folder of the module
	if err != nil {
		t.Fatal(err)
	}
	for _, goos := range []string{"linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			ctx := build.Default
			ctx.GOOS = goos
			seen := map[string]bool{}
			var walk func(path string)
			walk = func(path string) {
				if seen[path] {
					return
				}
				seen[path] = true
				p, err := ctx.ImportDir(filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(path, module))), 0)
				if err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				for _, imp := range p.Imports {
					if strings.HasPrefix(imp, module) {
						walk(imp)
					}
				}
			}
			walk(view)
			want := map[string]bool{view: true, "lifelog/internal/api/siren": true, "lifelog/internal/text": true}
			for path := range seen {
				if !want[path] {
					t.Errorf("the view imports %s: it may import only siren and text of this module", path)
				}
			}
			for path := range want {
				if !seen[path] {
					t.Errorf("the walk does not find %s: it reads the imports from the wrong folder", path)
				}
			}
		})
	}
}

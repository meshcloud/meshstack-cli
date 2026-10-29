package client

import (
	"go/importer"
	"go/token"
	"go/types"
	"io/fs"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// TestPublicApiNamesNoInternalType keeps client callable from another module, which Go's internal
// rule forbids to name an internal type.
func TestPublicApiNamesNoInternalType(t *testing.T) {
	imp := importer.ForCompiler(token.NewFileSet(), "source", nil).(types.ImporterFrom) //nolint:forcetypeassert // the source importer is one
	err := filepath.WalkDir(".", func(dir string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		if d.Name() == "internal" || d.Name() == "testdata" {
			return filepath.SkipDir
		}
		pkg, err := imp.ImportFrom(path.Join("github.com/meshcloud/meshstack-cli/client", filepath.ToSlash(dir)), ".", 0)
		if err != nil {
			return err
		}
		for _, name := range pkg.Scope().Names() {
			obj := pkg.Scope().Lookup(name)
			if !obj.Exported() {
				continue
			}
			check := surfaceCheck{t: t, seen: map[types.Type]bool{}}
			check.visit(obj.Pkg().Path()+"."+name, obj.Type())
			if named, ok := obj.Type().(*types.Named); ok {
				for method := range named.Methods() {
					if method.Exported() {
						check.visit(obj.Pkg().Path()+"."+name+"."+method.Name(), method.Type())
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

type surfaceCheck struct {
	t    *testing.T
	seen map[types.Type]bool
}

func (c surfaceCheck) visit(where string, typ types.Type) {
	if c.seen[typ] {
		return
	}
	c.seen[typ] = true
	switch typ := typ.(type) {
	case *types.Alias:
		c.visit(where, types.Unalias(typ))
	case *types.Named:
		if pkg := typ.Obj().Pkg(); pkg != nil && strings.Contains(pkg.Path()+"/", "/internal/") {
			c.t.Errorf("%s uses %s", where, typ)
		}
		for arg := range typ.TypeArgs().Types() {
			c.visit(where, arg)
		}
		if pkg := typ.Obj().Pkg(); pkg != nil && strings.HasPrefix(pkg.Path(), "github.com/meshcloud/meshstack-cli/client") {
			c.visit(where, typ.Underlying())
		}
	case *types.Pointer:
		c.visit(where, typ.Elem())
	case *types.Slice:
		c.visit(where, typ.Elem())
	case *types.Array:
		c.visit(where, typ.Elem())
	case *types.Chan:
		c.visit(where, typ.Elem())
	case *types.Map:
		c.visit(where, typ.Key())
		c.visit(where, typ.Elem())
	case *types.Signature:
		for param := range typ.Params().Variables() {
			c.visit(where, param.Type())
		}
		for result := range typ.Results().Variables() {
			c.visit(where, result.Type())
		}
	case *types.Struct:
		for field := range typ.Fields() {
			if field.Exported() {
				c.visit(where+"."+field.Name(), field.Type())
			}
		}
	case *types.Interface:
		for method := range typ.Methods() {
			if method.Exported() {
				c.visit(where+"."+method.Name(), method.Type())
			}
		}
	}
}

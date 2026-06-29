package testmain

import (
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

func ScanTestFuncs(pkg TestPackage) (*TestFuncs, error) {
	t := &TestFuncs{
		Package: pkg,
	}

	for _, file := range pkg.InternalTestFiles {
		if err := t.scanFile(filepath.Join(pkg.Dir, file), "_test", &t.ImportTest, &t.NeedTest); err != nil {
			return t, err
		}
	}

	for _, file := range pkg.ExternalTestFiles {
		if err := t.scanFile(filepath.Join(pkg.Dir, file), "_xtest", &t.ImportXtest, &t.NeedXtest); err != nil {
			return t, err
		}
	}

	return t, nil
}

var testFileSet = token.NewFileSet()

func (t *TestFuncs) scanFile(filename, pkg string, doImport, seen *bool) error {
	f, err := parser.ParseFile(testFileSet, filename, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("parsing %s: %w", filename, err)
	}

	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if fn.Recv != nil {
			continue
		}
		name := fn.Name.String()

		switch {
		case name == "TestMain":
			if isTestFunc(fn, "T") {
				t.Tests = append(t.Tests, TestFunc{Package: pkg, Name: name})
				*doImport = true
				*seen = true
				continue
			}
			if err := checkTestFunc(fn, "M"); err != nil {
				return err
			}
			if t.TestMain != nil {
				return fmt.Errorf("multiple definitions of TestMain")
			}
			t.TestMain = &TestFunc{Package: pkg, Name: name}
			*doImport = true
			*seen = true

		case isTest(name, "Test"):
			if err := checkTestFunc(fn, "T"); err != nil {
				return err
			}
			t.Tests = append(t.Tests, TestFunc{Package: pkg, Name: name})
			*doImport = true
			*seen = true

		case isTest(name, "Benchmark"):
			if err := checkTestFunc(fn, "B"); err != nil {
				return err
			}
			t.Benchmarks = append(t.Benchmarks, TestFunc{Package: pkg, Name: name})
			*doImport = true
			*seen = true

		case isTest(name, "Fuzz"):
			if err := checkTestFunc(fn, "F"); err != nil {
				return err
			}
			t.FuzzTargets = append(t.FuzzTargets, TestFunc{Package: pkg, Name: name})
			*doImport = true
			*seen = true
		}
	}

	examples := doc.Examples(f)
	sort.Slice(examples, func(i, j int) bool { return examples[i].Order < examples[j].Order })
	for _, e := range examples {
		*doImport = true
		if e.Output == "" && !e.EmptyOutput {
			continue
		}
		t.Examples = append(t.Examples, TestFunc{
			Package:   pkg,
			Name:      "Example" + e.Name,
			Output:    e.Output,
			Unordered: e.Unordered,
		})
		*seen = true
	}

	return nil
}

func isTest(name, prefix string) bool {
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	if len(name) == len(prefix) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(name[len(prefix):])
	return !unicode.IsLower(r)
}

func isTestFunc(fn *ast.FuncDecl, arg string) bool {
	if fn.Type.Results != nil && len(fn.Type.Results.List) > 0 {
		return false
	}
	if fn.Type.Params.List == nil || len(fn.Type.Params.List) != 1 {
		return false
	}
	if len(fn.Type.Params.List[0].Names) > 1 {
		return false
	}
	ptr, ok := fn.Type.Params.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	if name, ok := ptr.X.(*ast.Ident); ok && name.Name == arg {
		return true
	}
	if sel, ok := ptr.X.(*ast.SelectorExpr); ok && sel.Sel.Name == arg {
		return true
	}
	return false
}

func checkTestFunc(fn *ast.FuncDecl, arg string) error {
	var why string
	if !isTestFunc(fn, arg) {
		why = fmt.Sprintf("must be: func %s(%s *testing.%s)", fn.Name.String(), strings.ToLower(arg), arg)
	}
	if fn.Type.TypeParams.NumFields() > 0 {
		why = "test functions cannot have type parameters"
	}
	if why != "" {
		pos := testFileSet.Position(fn.Pos())
		return fmt.Errorf("%s: wrong signature for %s, %s", pos, fn.Name.String(), why)
	}
	return nil
}

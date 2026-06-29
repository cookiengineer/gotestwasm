package testmain

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

type TestPackage struct {
	ImportPath       string
	Dir              string
	InternalTestFiles []string
	ExternalTestFiles []string
	TestImports      []string
	XTestImports     []string
	ModulePath       string
}

type TestFunc struct {
	Package   string
	Name      string
	Output    string
	Unordered bool
}

type TestFuncs struct {
	Package     TestPackage
	Tests       []TestFunc
	Benchmarks  []TestFunc
	FuzzTargets []TestFunc
	Examples    []TestFunc
	TestMain    *TestFunc
	ImportTest  bool
	NeedTest    bool
	ImportXtest bool
	NeedXtest   bool
}

func DiscoverPackages(patterns []string, tags []string) ([]TestPackage, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedFiles |
			packages.NeedImports |
			packages.NeedModule,
		Tests: true,
	}

	if len(tags) > 0 {
		cfg.BuildFlags = make([]string, 0, len(tags))
		for _, tag := range tags {
			cfg.BuildFlags = append(cfg.BuildFlags, "-tags="+tag)
		}
	}

	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("failed to load packages: %w", err)
	}

	buildErrors := collectPackageErrors(pkgs)
	if len(buildErrors) > 0 {
		return nil, fmt.Errorf("package loading errors:\n%s", strings.Join(buildErrors, "\n"))
	}

	return collectTestPackages(pkgs), nil
}

func collectPackageErrors(pkgs []*packages.Package) []string {
	var errs []string
	for _, pkg := range pkgs {
		for _, e := range pkg.Errors {
			errs = append(errs, fmt.Sprintf("%s: %s", pkg.PkgPath, e))
		}
	}
	return errs
}

func isExternalTestPkg(pkg *packages.Package) bool {
	return strings.HasSuffix(pkg.PkgPath, "_test")
}

func isTestMainPkg(pkg *packages.Package) bool {
	return pkg.Name == "main" && strings.HasSuffix(pkg.PkgPath, ".test")
}

func isTestAugmented(pkg *packages.Package) bool {
	return strings.Contains(pkg.ID, " [") && strings.HasSuffix(pkg.ID, ".test]") && pkg.Name != "main"
}

func collectTestPackages(pkgs []*packages.Package) []TestPackage {
	seen := map[string]bool{}
	var result []TestPackage

	for _, pkg := range pkgs {
		if isTestMainPkg(pkg) {
			continue
		}

		if isExternalTestPkg(pkg) {
			basePkg := strings.TrimSuffix(pkg.PkgPath, "_test")
			tp := TestPackage{
				ImportPath:        basePkg,
				Dir:               pkg.Dir,
				ExternalTestFiles: relativeFilenames(pkg.GoFiles),
				XTestImports:      importMapKeys(pkg.Imports),
				ModulePath:        modulePath(pkg.Module),
			}
			mergeTestPackage(&result, tp)
			seen[basePkg] = true
			continue
		}

		if isTestAugmented(pkg) {
			internalTestFiles, _ := splitTestFiles(pkg.GoFiles)
			if seen[pkg.PkgPath] {
				mergeInternalFiles(&result, pkg.PkgPath, relativeFilenames(internalTestFiles), importMapKeys(pkg.Imports))
				continue
			}
			seen[pkg.PkgPath] = true

			tp := TestPackage{
				ImportPath:        pkg.PkgPath,
				Dir:               pkg.Dir,
				InternalTestFiles: relativeFilenames(internalTestFiles),
				TestImports:       importMapKeys(pkg.Imports),
				ModulePath:        modulePath(pkg.Module),
			}
			mergeTestPackage(&result, tp)
			continue
		}
	}

	for _, pkg := range pkgs {
		if isTestMainPkg(pkg) || isExternalTestPkg(pkg) || isTestAugmented(pkg) {
			continue
		}
		if seen[pkg.PkgPath] {
			continue
		}

		internalFiles, _ := splitTestFiles(pkg.GoFiles)
		externalFiles := findExternalTestFiles(pkgs, pkg.PkgPath)

		if len(internalFiles) == 0 && len(externalFiles) == 0 {
			continue
		}

		seen[pkg.PkgPath] = true
		tp := TestPackage{
			ImportPath:        pkg.PkgPath,
			Dir:               pkg.Dir,
			InternalTestFiles: relativeFilenames(internalFiles),
			ExternalTestFiles: externalFiles,
			TestImports:       importMapKeys(pkg.Imports),
			ModulePath:        modulePath(pkg.Module),
		}
		result = append(result, tp)
	}

	return result
}

func mergeTestPackage(result *[]TestPackage, tp TestPackage) {
	for i, existing := range *result {
		if existing.ImportPath == tp.ImportPath {
			if len(tp.InternalTestFiles) > 0 {
				(*result)[i].InternalTestFiles = append((*result)[i].InternalTestFiles, tp.InternalTestFiles...)
				(*result)[i].TestImports = append((*result)[i].TestImports, tp.TestImports...)
			}
			if len(tp.ExternalTestFiles) > 0 {
				(*result)[i].ExternalTestFiles = append((*result)[i].ExternalTestFiles, tp.ExternalTestFiles...)
				(*result)[i].XTestImports = append((*result)[i].XTestImports, tp.XTestImports...)
			}
			return
		}
	}
	*result = append(*result, tp)
}

func mergeInternalFiles(result *[]TestPackage, importPath string, files, imports []string) {
	for i, existing := range *result {
		if existing.ImportPath == importPath {
			(*result)[i].InternalTestFiles = append((*result)[i].InternalTestFiles, files...)
			(*result)[i].TestImports = append((*result)[i].TestImports, imports...)
			return
		}
	}
}

func splitTestFiles(files []string) (testFiles, sourceFiles []string) {
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			testFiles = append(testFiles, f)
		} else {
			sourceFiles = append(sourceFiles, f)
		}
	}
	return
}

func findExternalTestFiles(pkgs []*packages.Package, basePkg string) []string {
	extPkg := basePkg + "_test"
	for _, pkg := range pkgs {
		if pkg.PkgPath == extPkg && isExternalTestPkg(pkg) {
			return relativeFilenames(pkg.GoFiles)
		}
	}
	return nil
}

func relativeFilenames(files []string) []string {
	result := make([]string, 0, len(files))
	for _, f := range files {
		result = append(result, filepath.Base(f))
	}
	return result
}

func importMapKeys(m map[string]*packages.Package) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func modulePath(mod *packages.Module) string {
	if mod == nil {
		return ""
	}
	return mod.Path
}

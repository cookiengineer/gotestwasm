package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cookiengineer/gotestwasm/internal/testmain"
)

var (
	outputFile   string
	outputDir    string
	buildTags    string
	genOnly      bool
	verbose      bool
	ldflags      string
	gcflags      string
	printMain    bool
	runTests     bool
	chromiumPath string
	timeoutMs    int
)

func init() {
	flag.StringVar(&outputFile, "o", "tests.wasm", "output WASM file name")
	flag.StringVar(&outputDir, "outputdir", ".", "output directory for WASM files")
	flag.StringVar(&buildTags, "tags", "", "build tags (comma-separated)")
	flag.BoolVar(&genOnly, "gen", false, "generate _testmain.go only, skip build")
	flag.BoolVar(&verbose, "v", false, "verbose output")
	flag.StringVar(&ldflags, "ldflags", "", "extra linker flags")
	flag.StringVar(&gcflags, "gcflags", "", "extra compiler flags")
	flag.BoolVar(&printMain, "printmain", false, "print generated _testmain.go to stdout")
	flag.BoolVar(&runTests, "run", false, "run tests in headless Chromium after build")
	flag.StringVar(&chromiumPath, "chromium", "chromium", "path to Chromium binary")
	flag.IntVar(&timeoutMs, "timeout", 30000, "test timeout in milliseconds for headless run")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: gotestwasm [flags] [packages]\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  gotestwasm ./...                     build tests.wasm for all packages\n")
		fmt.Fprintf(os.Stderr, "  gotestwasm -o mytests.wasm .         custom output name\n")
		fmt.Fprintf(os.Stderr, "  gotestwasm -gen ./mypackage           generate _testmain.go only\n")
		fmt.Fprintf(os.Stderr, "  gotestwasm -tags=integration ./...    build with build tags\n")
		fmt.Fprintf(os.Stderr, "  gotestwasm -run -tags=wasm ./example  build and run in headless Chromium\n")
	}
}

func main() {
	flag.Parse()

	patterns := flag.Args()
	if len(patterns) == 0 {
		patterns = []string{"."}
	}

	var tagsList []string
	if buildTags != "" {
		tagsList = strings.Split(buildTags, ",")
	}

	config := testmain.BuildConfig{
		OutputDir: outputDir,
		BuildTags: tagsList,
	}

	if ldflags != "" {
		config.ExtraLdflags = strings.Split(ldflags, " ")
	}

	if gcflags != "" {
		config.ExtraGcflags = strings.Split(gcflags, " ")
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "Discovering test packages matching: %v\n", patterns)
	}

	pkgs, err := testmain.DiscoverPackages(patterns, tagsList)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(pkgs) == 0 {
		fmt.Fprintf(os.Stderr, "No packages with tests found.\n")
		os.Exit(1)
	}

	var allFuncs []testmain.TestFuncs
	for _, pkg := range pkgs {
		if verbose {
			fmt.Fprintf(os.Stderr, "Scanning %s ...\n", pkg.ImportPath)
		}

		funcs, err := testmain.ScanTestFuncs(pkg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error scanning %s: %v\n", pkg.ImportPath, err)
			os.Exit(1)
		}

		allFuncs = append(allFuncs, *funcs)

		if verbose {
			fmt.Fprintf(os.Stderr, "  %s: %d tests, %d benchmarks, %d fuzz, %d examples\n",
				pkg.ImportPath,
				len(funcs.Tests), len(funcs.Benchmarks), len(funcs.FuzzTargets), len(funcs.Examples))
		}
	}

	mainContent, err := testmain.GenerateTestmain(allFuncs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating _testmain.go: %v\n", err)
		os.Exit(1)
	}

	if printMain || genOnly {
		fmt.Print(string(mainContent))
	}

	if genOnly {
		return
	}

	generateTestmainFile(mainContent, outputDir)

	switch {
	case len(pkgs) == 1:
		err = buildSinglePackage(pkgs[0], config)
	default:
		err = buildMultiplePackages(pkgs, config)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if runTests {
		if len(pkgs) != 1 {
			fmt.Fprintf(os.Stderr, "Error: --run requires exactly one test package\n")
			os.Exit(1)
		}
		runTestInBrowser(pkgs[0], config)
	}
}

func buildSinglePackage(pkg testmain.TestPackage, config testmain.BuildConfig) error {
	if verbose {
		fmt.Fprintf(os.Stderr, "Building %s -> %s ...\n", pkg.ImportPath, outputFile)
	}

	result, err := testmain.BuildSingleWasm(pkg, outputFile, outputDir, config)
	if err != nil {
		return err
	}

	fmt.Printf("OK  %s -> %s\n", result.ImportPath, result.OutputPath)
	return nil
}

func buildMultiplePackages(pkgs []testmain.TestPackage, config testmain.BuildConfig) error {
	for _, pkg := range pkgs {
		if verbose {
			fmt.Fprintf(os.Stderr, "Building %s ...\n", pkg.ImportPath)
		}

		result, err := testmain.BuildWasm(pkg, config)
		if err != nil {
			return err
		}

		fmt.Printf("OK  %s -> %s\n", result.ImportPath, result.OutputPath)
	}

	return nil
}

func runTestInBrowser(pkg testmain.TestPackage, config testmain.BuildConfig) {
	wasmPath := filepath.Join(outputDir, outputFile)

	testDir, err := testmain.AssembleTestDir(outputDir, wasmPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error assembling test environment: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(testDir)

	fmt.Printf("\nTest directory: %s\n", testDir)

	runConfig := testmain.DefaultTestRunConfig()
	runConfig.ChromiumPath = chromiumPath
	runConfig.Timeout = time.Duration(timeoutMs) * time.Millisecond

	fmt.Printf("Running tests in headless Chromium...\n\n")

	passed, output, err := testmain.RunTests(testDir, runConfig)

	fmt.Print(output)

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running tests: %v\n", err)
		os.Exit(1)
	}

	if !passed {
		os.Exit(1)
	}
}

func generateTestmainFile(content []byte, dir string) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not create output directory: %v\n", err)
		return
	}

	path := filepath.Join(dir, "_testmain.go")
	err := os.WriteFile(path, content, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write _testmain.go: %v\n", err)
		return
	}

	if verbose {
		fmt.Fprintf(os.Stderr, "Generated %s\n", path)
	}
}

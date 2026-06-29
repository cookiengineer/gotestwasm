package testmain

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type BuildConfig struct {
	OutputDir   string
	BuildTags   []string
	ExtraLdflags []string
	ExtraGcflags []string
}

type BuildResult struct {
	OutputPath string
	ImportPath string
}

func BuildWasm(pkg TestPackage, config BuildConfig) (*BuildResult, error) {
	outputDir := config.OutputDir
	if outputDir == "" {
		outputDir = "."
	}

	binaryName := sanitizeBinaryName(pkg.ImportPath) + ".test.wasm"
	outputPath, err := filepath.Abs(filepath.Join(outputDir, binaryName))
	if err != nil {
		return nil, fmt.Errorf("resolving output path: %w", err)
	}

	args := []string{
		"test", "-c",
		"-o", outputPath,
	}

	for _, tag := range config.BuildTags {
		args = append(args, "-tags="+tag)
	}

	for _, ldflag := range config.ExtraLdflags {
		args = append(args, "-ldflags="+ldflag)
	}

	for _, gcflag := range config.ExtraGcflags {
		args = append(args, "-gcflags="+gcflag)
	}

	args = append(args, pkg.ImportPath)

	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(),
		"GOOS=js",
		"GOARCH=wasm",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go test -c failed for %s: %s\n%s", pkg.ImportPath, err, string(output))
	}

	return &BuildResult{
		OutputPath: outputPath,
		ImportPath: pkg.ImportPath,
	}, nil
}

func BuildSingleWasm(pkg TestPackage, outputName, outputDir string, config BuildConfig) (*BuildResult, error) {
	if outputDir == "" {
		outputDir = "."
	}

	if outputName == "" {
		outputName = "tests.wasm"
	}

	outputPath, err := filepath.Abs(filepath.Join(outputDir, outputName))
	if err != nil {
		return nil, fmt.Errorf("resolving output path: %w", err)
	}

	args := []string{
		"test", "-c",
		"-o", outputPath,
	}

	for _, tag := range config.BuildTags {
		args = append(args, "-tags="+tag)
	}

	for _, ldflag := range config.ExtraLdflags {
		args = append(args, "-ldflags="+ldflag)
	}

	for _, gcflag := range config.ExtraGcflags {
		args = append(args, "-gcflags="+gcflag)
	}

	args = append(args, pkg.ImportPath)

	cmd := exec.Command("go", args...)
	cmd.Env = append(os.Environ(),
		"GOOS=js",
		"GOARCH=wasm",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("go test -c failed for %s: %s\n%s", pkg.ImportPath, err, string(output))
	}

	return &BuildResult{
		OutputPath: outputPath,
		ImportPath: pkg.ImportPath,
	}, nil
}

func sanitizeBinaryName(importPath string) string {
	name := strings.ReplaceAll(importPath, "/", "_")
	name = strings.ReplaceAll(name, ".", "_")
	name = strings.ReplaceAll(name, "-", "_")
	return name
}

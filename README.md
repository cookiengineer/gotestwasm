# gotestwasm

`gotestwasm` compiles Go tests into standalone WebAssembly binaries and executes them in a headless browser. It reuses the Go toolchain for compilation and linking while adding the browser runtime layer that `go test` does not provide for the `GOOS=js GOARCH=wasm` target.

## Problem

`go test -c` compiles a test binary for the host platform. When cross-compiling with `GOOS=js GOARCH=wasm`, the resulting `.wasm` file has no runtime environment — the Go standard library's `js` port expects a JavaScript host (Node.js or a browser) with `wasm_exec.js` to provide system call implementations. Running `go test` directly for this target is not supported because `go test` attempts to execute the binary natively.

`gotestwasm` bridges this gap: it compiles the test binary with `go test -c`, assembles a browser environment around it (`wasm_exec.js` + `index.html`), and runs it through a headless Chromium instance. The result is a test workflow that behaves like `go test -v` but executes inside a real browser's JavaScript runtime.

## Comparison with upstream

| Capability                     | `go test`       | `gotestwasm`                      |
|--------------------------------|-----------------|-----------------------------------|
| Compiles test binary           | yes             | yes (delegates to `go test -c`)   |
| Executes binary natively       | yes             | no                                |
| Executes binary in browser     | no              | yes                               |
| `//go:build wasm` test files   | skipped on host | compiled and run                  |
| `syscall/js` in tests          | not available   | fully supported                   |
| Headless CI execution          | yes             | yes (via Chromium)                |
| Locates `wasm_exec.js`         | N/A             | automatic from `GOROOT`           |
| Generates `_testmain.go`       | internal only   | written to disk for inspection    |

## How it works

```
gotestwasm ./mypackage
```

1. **Package discovery** — Uses `golang.org/x/tools/go/packages` to resolve module-aware import paths, build tags, and identify all `*_test.go` files. Internal test files (`package pkg`) and external test files (`package pkg_test`) are tracked separately.

2. **Test function scanning** — Parses each `*_test.go` file with `go/parser` and `go/ast` to extract `Test*`, `Benchmark*`, `Fuzz*`, `TestMain`, and `Example*` functions. Function signatures are validated against Go's test function conventions.

3. **Testmain generation** — Produces a `_testmain.go` file using the same template structure as `cmd/go`. This file imports all test packages, registers discovered test functions with `testing.MainStart`, and provides the `main()` entry point. The generated file is written alongside the output for inspection and customization.

4. **Compilation** — Shells out to `go test -c -o tests.wasm` with `GOOS=js GOARCH=wasm`. This reuses the full Go toolchain: compiler, linker, module resolution, build cache, and PGO. The `go test -c` path is required because it handles the `testing/internal/testdeps` import that `go build` rejects.

5. **Runtime assembly** — Creates a `/tmp/go-test-*` directory containing:
   - `tests.wasm` — the compiled test binary
   - `wasm_exec.js` — the Go JS runtime, located automatically from `GOROOT/lib/wasm/`
   - `index.html` — a bootstrap page that loads the WASM, intercepts console output, and reports results

6. **Headless execution** (with `--run`) — Starts an embedded HTTP server serving the test directory, launches Chromium in headless mode with `--window-size=1280,1024`, waits for the test binary to report its exit status via `fetch("/report")`, captures the full console output, and returns a matching exit code.

## Installation

```
go install github.com/cookiengineer/gotestwasm/cmds/gotestwasm@latest
```

Requires Go 1.21 or later. The headless execution feature (`--run`) requires Chromium in `$PATH`.

## Usage

```
gotestwasm [flags] [packages]
```

### Flags

| Flag           | Default        | Description                                          |
|----------------|----------------|------------------------------------------------------|
| `-o`           | `tests.wasm`   | Output WASM file name                                |
| `-outputdir`   | `.`            | Output directory for WASM and `_testmain.go`         |
| `-tags`        |                | Build tags, comma-separated                          |
| `-gen`         | `false`        | Generate `_testmain.go` only, skip build             |
| `-printmain`   | `false`        | Print generated `_testmain.go` to stdout             |
| `-v`           | `false`        | Verbose output                                       |
| `-ldflags`     |                | Extra linker flags                                   |
| `-gcflags`     |                | Extra compiler flags                                 |
| `-run`         | `false`        | Build and execute in headless Chromium               |
| `-chromium`    | `chromium`     | Path to Chromium binary                              |
| `-timeout`     | `30000`        | Test timeout in milliseconds (headless mode)         |

### Examples

Build a test binary:

```
$ gotestwasm ./mypackage
OK  mypackage -> /home/user/mypackage/tests.wasm
```

Build with build tags:

```
$ gotestwasm -tags=integration ./...
```

Generate `_testmain.go` for inspection:

```
$ gotestwasm -gen -printmain ./mypackage
// Code generated by gotestwasm. DO NOT EDIT.

package main

import (
    "os"
    "reflect"
    "testing"
    "testing/internal/testdeps"
    _test "mypackage"
)

var tests = []testing.InternalTest{
    {"TestFoo", _test.TestFoo},
    ...
```

Build and run in headless Chromium:

```
$ gotestwasm -run -tags=wasm ./example
=== RUN   TestWindow
    window_test.go:19: window innerWidth=1280, innerHeight=885
--- PASS: TestWindow (0.00s)
PASS
```

## Example project

The `./example` directory contains a browser-targeted test:

```go
//go:build wasm

package example

import (
    "syscall/js"
    "testing"
)

func TestWindow(t *testing.T) {
    window := js.Global().Get("window")
    if window.IsUndefined() || window.IsNull() {
        t.Skip("window is not available in this runtime")
    }

    width := window.Get("innerWidth").Int()
    height := window.Get("innerHeight").Int()

    t.Logf("window innerWidth=%d, innerHeight=%d", width, height)

    if width <= 1024 {
        t.Errorf("expected window.innerWidth > 1024, got %d", width)
    }
    if height <= 768 {
        t.Errorf("expected window.innerHeight > 768, got %d", height)
    }
}
```

The `//go:build wasm` constraint ensures this test is skipped on the host platform and only compiled when targeting WASM. The test accesses browser DOM APIs through `syscall/js` to assert viewport dimensions. Build and run it with:

```
gotestwasm -run -tags=wasm ./example
```

## Generated testmain

The generated `_testmain.go` follows the same structure as the test harness produced internally by `go test`. It imports each test package with a distinct alias (`_test` for internal tests, `_xtest` for external `package pkg_test` tests), registers discovered functions into the `testing.InternalTest` / `InternalBenchmark` / `InternalFuzzTarget` / `InternalExample` slices, and calls `testing.MainStart` in `main()`. TestMain is supported: if a `TestMain` function is detected, the generated code calls it with the `*testing.M` value and reads the exit code from its reflected `exitCode` field, matching the behavior of `go test`.

## License

WTFPL

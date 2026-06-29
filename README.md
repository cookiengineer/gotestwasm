
# gotestwasm


## Problem

- `go test -c` doesn't work with `//go:build wasm` tagged Go files

## Features

- [x] `gotestwasm` generates a `_testmain.go` which then is compiled to a `.wasm` file
- [ ] `gotestwasm` generates an `index.html` which then is opened via chromium
- [ ] `gotestwasm` uses chromium's debug protocol for the status of failed/succeeded tests

## License

WTFPL


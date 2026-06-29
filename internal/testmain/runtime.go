package testmain

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const indexHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>running</title>
</head>
<body>
<pre id="output"></pre>
<div id="status">running...</div>
<script src="wasm_exec.js"></script>
<script>
(function() {
	var output = document.getElementById("output");
	var status = document.getElementById("status");

	function append(text) {
		output.textContent += text;
	}

	console.log = function() {
		var args = Array.prototype.slice.call(arguments);
		append(args.join(" ") + "\n");
	};
	console.warn = function() {
		var args = Array.prototype.slice.call(arguments);
		append("WARN: " + args.join(" ") + "\n");
	};
	console.error = function() {
		var args = Array.prototype.slice.call(arguments);
		append("ERROR: " + args.join(" ") + "\n");
	};

	var go = new Go();

	go.exit = function(code) {
		var result = code === 0 ? "PASS" : "FAIL";
		status.textContent = result + " (exit " + code + ")";
		document.title = result;
		fetch("/report", {
			method: "POST",
			body: result + "\n" + output.textContent
		});
	};

	fetch("tests.wasm")
		.then(function(response) { return response.arrayBuffer(); })
		.then(function(bytes) { return WebAssembly.instantiate(bytes, go.importObject); })
		.then(function(result) { go.run(result.instance); })
		.catch(function(err) {
			append("FAIL: " + err.message + "\n");
			status.textContent = "FAIL";
			document.title = "FAIL";
			fetch("/report", {
				method: "POST",
				body: "FAIL\n" + output.textContent + "\n" + err.message
			});
		});
})();
</script>
</body>
</html>`

type TestRunConfig struct {
	ChromiumPath   string
	ViewportWidth  int
	ViewportHeight int
	Timeout        time.Duration
}

func DefaultTestRunConfig() TestRunConfig {
	return TestRunConfig{
		ChromiumPath:   "chromium",
		ViewportWidth:  1280,
		ViewportHeight: 1024,
		Timeout:        30 * time.Second,
	}
}

type TestReport struct {
	Title    string `json:"title"`
	Text     string `json:"text"`
	ExitCode int    `json:"exitCode"`
}

func FindWasmExec() (string, error) {
	goroot := os.Getenv("GOROOT")
	if goroot == "" {
		out, err := exec.Command("go", "env", "GOROOT").Output()
		if err != nil {
			return "", fmt.Errorf("cannot determine GOROOT: %w", err)
		}
		goroot = strings.TrimSpace(string(out))
	}

	for _, p := range []string{
		filepath.Join(goroot, "lib", "wasm", "wasm_exec.js"),
		filepath.Join(goroot, "misc", "wasm", "wasm_exec.js"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	return "", fmt.Errorf("wasm_exec.js not found in GOROOT (%s)", goroot)
}

func AssembleTestDir(outputDir, wasmPath string) (string, error) {
	testDir, err := os.MkdirTemp("", "go-test-*")
	if err != nil {
		return "", fmt.Errorf("creating temp test directory: %w", err)
	}

	if err := copyFile(wasmPath, filepath.Join(testDir, "tests.wasm")); err != nil {
		os.RemoveAll(testDir)
		return "", fmt.Errorf("copying tests.wasm: %w", err)
	}

	wasmExecPath, err := FindWasmExec()
	if err != nil {
		os.RemoveAll(testDir)
		return "", err
	}

	if err := copyFile(wasmExecPath, filepath.Join(testDir, "wasm_exec.js")); err != nil {
		os.RemoveAll(testDir)
		return "", fmt.Errorf("copying wasm_exec.js: %w", err)
	}

	if err := os.WriteFile(filepath.Join(testDir, "index.html"), []byte(indexHTML), 0644); err != nil {
		os.RemoveAll(testDir)
		return "", fmt.Errorf("writing index.html: %w", err)
	}

	if outputDir != "." && outputDir != "" {
		exportDir := filepath.Join(outputDir, filepath.Base(testDir))
		if err := os.MkdirAll(exportDir, 0755); err == nil {
			copyFile(filepath.Join(testDir, "tests.wasm"), filepath.Join(exportDir, "tests.wasm"))
			copyFile(filepath.Join(testDir, "wasm_exec.js"), filepath.Join(exportDir, "wasm_exec.js"))
			copyFile(filepath.Join(testDir, "index.html"), filepath.Join(exportDir, "index.html"))
		}
	}

	return testDir, nil
}

func RunTests(testDir string, config TestRunConfig) (bool, string, error) {
	reportCh := make(chan TestReport, 1)
	serverDone := make(chan struct{})

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(testDir)))
	mux.HandleFunc("/report", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return
		}

		report := TestReport{}
		bodyStr := string(body)
		if nl := strings.IndexByte(bodyStr, '\n'); nl >= 0 {
			report.Title = strings.TrimSpace(bodyStr[:nl])
			report.Text = bodyStr[nl+1:]
		} else {
			report.Title = strings.TrimSpace(bodyStr)
		}

		reportCh <- report
		w.WriteHeader(http.StatusOK)
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return false, "", fmt.Errorf("starting test server: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	url := fmt.Sprintf("http://127.0.0.1:%d/index.html", port)

	server := &http.Server{Handler: mux}
	go func() {
		server.Serve(listener)
		close(serverDone)
	}()
	defer func() {
		server.Shutdown(context.Background())
		<-serverDone
	}()

	chromium := config.ChromiumPath
	if chromium == "" {
		chromium = "chromium"
	}

	viewport := fmt.Sprintf("%d,%d", config.ViewportWidth, config.ViewportHeight)

	args := []string{
		"--headless",
		"--no-sandbox",
		"--disable-gpu",
		"--disable-dev-shm-usage",
		"--window-size=" + viewport,
		url,
	}

	time.Sleep(100 * time.Millisecond)

	cmd := exec.Command(chromium, args...)
	cmd.Env = os.Environ()

	var chromeOutput []byte
	var cmdErr error
	cmdDone := make(chan struct{})

	go func() {
		chromeOutput, cmdErr = cmd.CombinedOutput()
		close(cmdDone)
	}()

	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	var report TestReport
	var reportReceived bool

	select {
	case report = <-reportCh:
		reportReceived = true
		cmd.Process.Kill()
	case <-cmdDone:
	case <-time.After(timeout):
		cmd.Process.Kill()
		return false, string(chromeOutput), fmt.Errorf("test timed out after %v", timeout)
	}

	<-cmdDone
	_ = cmdErr

	if reportReceived {
		passed := report.Title == "PASS"
		return passed, report.Text, nil
	}

	return false, string(chromeOutput), fmt.Errorf("no test report received\n%s", string(chromeOutput))
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

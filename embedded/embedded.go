package embedded

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

//go:embed VERSION.txt
var version string

// init extracts the embedded native library to a temporary directory and sets
// the environment so the webview package can find it at runtime. This is a
// justified exception to the "no init() side effects" guideline (AGENTS.md §4.3)
// because it is the core mechanism behind "import _ embedded" convenience.
func init() {
	dir := filepath.Join(os.TempDir(), "webview-"+version)
	file := filepath.Join(dir, name)

	if _, err := os.Stat(file); err != nil {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			fmt.Fprintf(os.Stderr, "webview/embedded: failed to create directory %s: %v\n", dir, err)
			os.Exit(1)
		}
		if err := os.WriteFile(file, lib, os.ModePerm); err != nil { //nolint:gosec
			fmt.Fprintf(os.Stderr, "webview/embedded: failed to write library %s: %v\n", file, err)
			os.Exit(1)
		}
	}

	if runtime.GOOS == "windows" {
		if err := os.Setenv("PATH", dir+";"+os.Getenv("PATH")); err != nil {
			fmt.Fprintf(os.Stderr, "webview/embedded: failed to set PATH: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := os.Setenv("WEBVIEW_PATH", dir); err != nil {
			fmt.Fprintf(os.Stderr, "webview/embedded: failed to set WEBVIEW_PATH: %v\n", err)
			os.Exit(1)
		}
	}
}

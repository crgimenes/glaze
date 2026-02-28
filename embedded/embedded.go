package embedded

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

//go:embed VERSION.txt
var version string

var extractOnce sync.Once
var extractErr error

// Extract writes the embedded native library to a temporary directory and sets
// the environment so the glaze package can find it at runtime. It is safe to
// call multiple times; only the first call has effect. Returns an error instead
// of calling os.Exit so the caller can handle failures gracefully.
func Extract() error {
	extractOnce.Do(func() {
		dir := filepath.Join(os.TempDir(), "webview-"+version)
		file := filepath.Join(dir, name)

		if _, err := os.Stat(file); err != nil {
			if err := os.MkdirAll(dir, os.ModePerm); err != nil {
				extractErr = fmt.Errorf("webview/embedded: failed to create directory %s: %w", dir, err)
				return
			}
			if err := os.WriteFile(file, lib, os.ModePerm); err != nil { //nolint:gosec
				extractErr = fmt.Errorf("webview/embedded: failed to write library %s: %w", file, err)
				return
			}
		}

		if runtime.GOOS == "windows" {
			if err := os.Setenv("PATH", dir+";"+os.Getenv("PATH")); err != nil {
				extractErr = fmt.Errorf("webview/embedded: failed to set PATH: %w", err)
			}
			return
		}
		if err := os.Setenv("WEBVIEW_PATH", dir); err != nil {
			extractErr = fmt.Errorf("webview/embedded: failed to set WEBVIEW_PATH: %w", err)
		}
	})
	return extractErr
}

// init calls Extract for backward compatibility with the "import _ embedded"
// pattern. New code should call Extract() explicitly before glaze.Init().
func init() {
	if err := Extract(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

package embedded

import (
	_ "embed"
	"os"
	"path/filepath"
	"runtime"
)

//go:embed VERSION.txt
var version string

func init() {
	dir := filepath.Join(os.TempDir(), "webview-"+version)
	file := filepath.Join(dir, name)

	if _, err := os.Stat(file); err != nil {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			panic(err)
		}
		if err := os.WriteFile(file, lib, os.ModePerm); err != nil { //nolint:gosec
			panic(err)
		}
	}

	if runtime.GOOS == "windows" {
		if err := os.Setenv("PATH", dir+";"+os.Getenv("PATH")); err != nil {
			panic(err)
		}
	} else {
		if err := os.Setenv("WEBVIEW_PATH", dir); err != nil {
			panic(err)
		}
	}
}

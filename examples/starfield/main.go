package main

import (
	"embed"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"

	"github.com/crgimenes/glaze"
	_ "github.com/crgimenes/glaze/embedded"
)

//go:embed ui/index.html ui/app.css ui/app.js
var uiFS embed.FS

func stageUIFiles() (string, string, error) {
	tmpDir, err := os.MkdirTemp("", "glaze-starfield-*")
	if err != nil {
		return "", "", fmt.Errorf("create temp dir: %w", err)
	}

	files := []string{"ui/index.html", "ui/app.css", "ui/app.js"}
	for _, name := range files {
		data, readErr := uiFS.ReadFile(name)
		if readErr != nil {
			_ = os.RemoveAll(tmpDir)
			return "", "", fmt.Errorf("read embedded file %s: %w", name, readErr)
		}

		base := filepath.Base(name)
		target := filepath.Join(tmpDir, base)
		if writeErr := os.WriteFile(target, data, 0o600); writeErr != nil {
			_ = os.RemoveAll(tmpDir)
			return "", "", fmt.Errorf("write ui file %s: %w", target, writeErr)
		}
	}

	indexPath := filepath.Join(tmpDir, "index.html")
	indexURL := (&url.URL{Scheme: "file", Path: indexPath}).String()
	return tmpDir, indexURL, nil
}

func main() {
	w, err := glaze.New(true)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Destroy()

	w.SetTitle("Starfield")
	w.SetSize(1024, 768, glaze.HintNone)

	uiDir, indexURL, err := stageUIFiles()
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(uiDir)

	w.Navigate(indexURL)
	w.Run()
}

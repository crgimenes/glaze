package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"

	"github.com/crgimenes/glaze"
	_ "github.com/crgimenes/glaze/embedded"
)

//go:embed ui/index.html ui/app.css ui/app.js
var uiFS embed.FS

// startServer starts a local HTTP server on a random loopback port
// serving the embedded UI files. Returns the base URL.
func startServer() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("listen: %w", err)
	}

	sub, err := fs.Sub(uiFS, "ui")
	if err != nil {
		return "", fmt.Errorf("sub fs: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(sub)))

	go func() {
		srv := &http.Server{Handler: mux}
		_ = srv.Serve(ln)
	}()

	addr := ln.Addr().(*net.TCPAddr)
	return fmt.Sprintf("http://127.0.0.1:%d", addr.Port), nil
}

func main() {
	w, err := glaze.New(true)
	if err != nil {
		log.Fatal(err)
	}
	defer w.Destroy()

	w.SetTitle("Ray Casting Engine")
	w.SetSize(1280, 820, glaze.HintNone)

	baseURL, err := startServer()
	if err != nil {
		log.Fatal(err)
	}

	w.Navigate(baseURL)
	w.Run()
}

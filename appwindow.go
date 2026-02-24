package glaze

import (
	"fmt"
	"net"
	"net/http"
)

// AppOptions configures an AppWindow.
type AppOptions struct {
	// Title is the window title.
	Title string

	// Width and Height set the initial window dimensions.
	Width  int
	Height int

	// Hint controls window resize behaviour (HintNone, HintMin, HintMax, HintFixed).
	Hint Hint

	// Debug enables the browser developer tools.
	Debug bool

	// Addr is the listen address for the local HTTP server.
	// Defaults to "127.0.0.1:0" (random port on loopback).
	Addr string

	// Handler is the HTTP handler to serve (typically an http.ServeMux).
	Handler http.Handler

	// OnReady is called once the server is listening, with the base URL.
	// Use it to log the address or perform additional setup.
	OnReady func(addr string)
}

// AppWindow creates a native window backed by a local HTTP server.
//
// It starts the server on a random loopback port (or the address specified
// in opts.Addr), opens a webview pointing to it, and runs the UI event loop.
// When the user closes the window, the server is shut down and AppWindow returns.
//
// This is the recommended way to wrap a full devengine application as a
// desktop app — pass the configured http.ServeMux as opts.Handler and
// everything (templates, assets, routes) works unmodified.
func AppWindow(opts AppOptions) error {
	if opts.Handler == nil {
		return fmt.Errorf("webview: AppOptions.Handler must not be nil")
	}
	if opts.Width <= 0 {
		opts.Width = 1024
	}
	if opts.Height <= 0 {
		opts.Height = 768
	}
	if opts.Addr == "" {
		opts.Addr = "127.0.0.1:0"
	}
	if opts.Title == "" {
		opts.Title = "App"
	}

	// Bind to a free port.
	ln, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		return fmt.Errorf("webview: listen %s: %w", opts.Addr, err)
	}

	addr := ln.Addr().(*net.TCPAddr)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", addr.Port)

	if opts.OnReady != nil {
		opts.OnReady(baseURL)
	}

	// Start the HTTP server in the background.
	srv := &http.Server{Handler: opts.Handler}
	go func() { _ = srv.Serve(ln) }()

	// Create the webview window.
	w, err := New(opts.Debug)
	if err != nil {
		_ = srv.Close()
		return fmt.Errorf("webview: %w", err)
	}

	w.SetTitle(opts.Title)
	w.SetSize(opts.Width, opts.Height, opts.Hint)
	w.Navigate(baseURL)
	w.Run()
	w.Destroy()

	// Window closed — shut down the server.
	_ = srv.Close()
	return nil
}

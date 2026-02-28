package glaze

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"runtime"
)

// AppTransport selects how AppWindow serves HTTP to the embedded browser.
type AppTransport string

const (
	// AppTransportAuto chooses the recommended platform default.
	// - macOS/Linux: unix backend socket with loopback HTTP gateway.
	// - Windows: loopback TCP.
	AppTransportAuto AppTransport = "auto"

	// AppTransportTCP serves directly over loopback TCP.
	AppTransportTCP AppTransport = "tcp"

	// AppTransportUnix serves the application handler over a Unix domain socket.
	// A lightweight loopback HTTP gateway is created so the embedded browser can
	// still navigate with a standard http:// URL.
	AppTransportUnix AppTransport = "unix"
)

// AppReadyInfo contains transport details once AppWindow listeners are ready.
type AppReadyInfo struct {
	// URL is the navigable URL used by the embedded browser.
	URL string

	// Transport is the resolved backend transport in use.
	Transport AppTransport

	// Backend is the backend listener endpoint.
	// - tcp: "ip:port"
	// - unix: "/path/to/socket"
	Backend string

	// Gateway is the loopback gateway endpoint when unix transport is used.
	// For tcp transport this matches Backend.
	Gateway string
}

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

	// Transport selects the backend transport.
	// Defaults to AppTransportAuto.
	Transport AppTransport

	// Addr is the listen address for the local HTTP server.
	// Used by AppTransportTCP and defaults to "127.0.0.1:0".
	Addr string

	// UnixSocketPath is an optional socket path used when Transport is unix.
	// If empty, a temporary socket path is generated automatically.
	UnixSocketPath string

	// Handler is the HTTP handler to serve (typically an http.ServeMux).
	Handler http.Handler

	// OnReady is called once listeners are up, with the navigable base URL.
	// Use it to log the address or perform additional setup.
	OnReady func(addr string)

	// OnReadyInfo is called once listeners are up, with transport details.
	// This is useful to inspect whether backend transport is tcp or unix.
	OnReadyInfo func(info AppReadyInfo)
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
	if opts.Title == "" {
		opts.Title = "App"
	}

	setup, err := setupAppTransport(opts)
	if err != nil {
		return err
	}
	defer func() {
		if setup.close != nil {
			_ = setup.close()
		}
	}()

	// Start extra transport components (for example, Unix loopback gateway).
	setup.start()

	// Start the application HTTP server in the background.
	srv := &http.Server{Handler: opts.Handler}
	defer func() { _ = srv.Close() }()
	go func() { _ = srv.Serve(setup.listener) }()

	if opts.OnReady != nil {
		opts.OnReady(setup.baseURL)
	}
	if opts.OnReadyInfo != nil {
		opts.OnReadyInfo(AppReadyInfo{
			URL:       setup.baseURL,
			Transport: setup.transport,
			Backend:   setup.backend,
			Gateway:   setup.gateway,
		})
	}

	// Create the webview window.
	w, err := New(opts.Debug)
	if err != nil {
		return fmt.Errorf("webview: %w", err)
	}

	w.SetTitle(opts.Title)
	w.SetSize(opts.Width, opts.Height, opts.Hint)
	w.Navigate(setup.baseURL)
	w.Run()
	w.Destroy()

	return nil
}

type appTransportSetup struct {
	listener  net.Listener
	baseURL   string
	transport AppTransport
	backend   string
	gateway   string
	start     func()
	close     func() error
}

func setupAppTransport(opts AppOptions) (appTransportSetup, error) {
	transport, err := resolveAppTransport(opts.Transport, runtime.GOOS)
	if err != nil {
		return appTransportSetup{}, err
	}

	switch transport {
	case AppTransportTCP:
		return setupTCPTransport(opts.Addr)
	case AppTransportUnix:
		return setupUnixTransport(opts.UnixSocketPath)
	default:
		return appTransportSetup{}, fmt.Errorf("webview: unsupported transport %q", transport)
	}
}

func resolveAppTransport(requested AppTransport, goos string) (AppTransport, error) {
	switch {
	case requested == "" || requested == AppTransportAuto:
		if goos == "windows" {
			return AppTransportTCP, nil
		}
		return AppTransportUnix, nil
	case requested == AppTransportTCP:
		return AppTransportTCP, nil
	case requested == AppTransportUnix:
		if goos == "windows" {
			return "", errors.New("webview: unix transport is not supported on windows")
		}
		return AppTransportUnix, nil
	default:
		return "", fmt.Errorf("webview: invalid transport %q", requested)
	}
}

func setupTCPTransport(addr string) (appTransportSetup, error) {
	if addr == "" {
		addr = "127.0.0.1:0"
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return appTransportSetup{}, fmt.Errorf("webview: listen %s: %w", addr, err)
	}

	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()
		return appTransportSetup{}, errors.New("webview: failed to read tcp listen address")
	}

	return appTransportSetup{
		listener:  ln,
		baseURL:   fmt.Sprintf("http://127.0.0.1:%d", tcpAddr.Port),
		transport: AppTransportTCP,
		backend:   tcpAddr.String(),
		gateway:   tcpAddr.String(),
		start:     func() {},
		close:     nil,
	}, nil
}

func setupUnixTransport(socketPath string) (appTransportSetup, error) {
	path, err := prepareUnixSocketPath(socketPath)
	if err != nil {
		return appTransportSetup{}, err
	}

	unixListener, err := net.Listen("unix", path)
	if err != nil {
		_ = removeUnixSocket(path)
		return appTransportSetup{}, fmt.Errorf("webview: listen unix %s: %w", path, err)
	}

	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = unixListener.Close()
		_ = removeUnixSocket(path)
		return appTransportSetup{}, fmt.Errorf("webview: listen tcp gateway: %w", err)
	}

	proxyURL := &url.URL{Scheme: "http", Host: "unix"}
	proxy := httputil.NewSingleHostReverseProxy(proxyURL)
	proxy.Transport = &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", path)
		},
	}
	proxyServer := &http.Server{Handler: proxy}

	tcpAddr, ok := proxyListener.Addr().(*net.TCPAddr)
	if !ok {
		_ = proxyServer.Close()
		_ = proxyListener.Close()
		_ = unixListener.Close()
		_ = removeUnixSocket(path)
		return appTransportSetup{}, errors.New("webview: failed to read tcp gateway address")
	}

	return appTransportSetup{
		listener:  unixListener,
		baseURL:   fmt.Sprintf("http://127.0.0.1:%d", tcpAddr.Port),
		transport: AppTransportUnix,
		backend:   path,
		gateway:   tcpAddr.String(),
		start: func() {
			go func() { _ = proxyServer.Serve(proxyListener) }()
		},
		close: func() error {
			_ = proxyServer.Close()
			_ = proxyListener.Close()
			return removeUnixSocket(path)
		},
	}, nil
}

func prepareUnixSocketPath(socketPath string) (string, error) {
	path := socketPath
	if path == "" {
		tmpFile, err := os.CreateTemp("", "glaze-*.sock")
		if err != nil {
			return "", fmt.Errorf("webview: create temp socket path: %w", err)
		}
		path = tmpFile.Name()
		if err := tmpFile.Close(); err != nil {
			return "", fmt.Errorf("webview: close temp file: %w", err)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("webview: remove temp file %s: %w", path, err)
		}
		return path, nil
	}

	if err := removeUnixSocket(path); err != nil {
		return "", err
	}
	return path, nil
}

func removeUnixSocket(path string) error {
	if path == "" {
		return nil
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("webview: stat unix socket %s: %w", path, err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("webview: %s exists and is not a unix socket", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("webview: remove unix socket %s: %w", path, err)
	}
	return nil
}

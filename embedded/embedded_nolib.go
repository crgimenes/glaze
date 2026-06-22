//go:build !windows

// On macOS and Linux, glaze uses a pure-Go WebView backend (WKWebView /
// WebKitGTK) and loads no native library, so there is nothing to embed or
// extract. Extract/ExtractTo are kept as no-ops to preserve the package API
// (including the `import _` side-effect pattern). Only Windows still embeds and
// extracts a native library (WebView2 loader path notwithstanding).

package embedded

// ExtractTo is a no-op on platforms with a pure-Go backend.
func ExtractTo(dir string) error { return nil }

// Extract is a no-op on platforms with a pure-Go backend. See ExtractTo.
func Extract() error { return nil }

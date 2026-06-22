// glaze uses a pure-Go WebView backend on every platform (WKWebView on macOS,
// WebKitGTK on Linux, WebView2/COM on Windows) and loads no bundled native
// library, so there is nothing to embed or extract. Extract/ExtractTo are kept
// as no-ops to preserve the package API (including the `import _` side-effect
// pattern that existing code uses).

package embedded

// ExtractTo is a no-op: no native library is bundled.
func ExtractTo(dir string) error { return nil }

// Extract is a no-op: no native library is bundled. See ExtractTo.
func Extract() error { return nil }

// On macOS, glaze uses a pure-Go WebKit backend and loads no native library,
// so there is nothing to embed or extract. Extract/ExtractTo are kept as no-ops
// to preserve the package API (including the `import _` side-effect pattern).

package embedded

// ExtractTo is a no-op on macOS; the glaze backend calls the system frameworks
// directly and needs no extracted native library.
func ExtractTo(dir string) error { return nil }

// Extract is a no-op on macOS. See ExtractTo.
func Extract() error { return nil }

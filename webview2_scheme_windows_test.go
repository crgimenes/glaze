package glaze

import "testing"

// rewriteSchemeURL maps a registered scheme's URL to its https vhost and leaves
// everything else alone. Pure string logic, so it runs headless.
func TestRewriteSchemeURL(t *testing.T) {
	w := &webview{schemeHandlers: map[string]SchemeHandler{"app": nil}}

	cases := []struct {
		name, in, want string
	}{
		{"registered scheme -> vhost", "app://home/index.html", "https://app.localhost/index.html"},
		{"keeps query", "app://home/x?y=1", "https://app.localhost/x?y=1"},
		{"keeps fragment", "app://home/index.html#/route", "https://app.localhost/index.html#/route"},
		{"keeps query and fragment", "app://home/x?y=1#/r", "https://app.localhost/x?y=1#/r"},
		{"root path", "app://home/", "https://app.localhost/"},
		{"unregistered scheme passes through", "other://z/a", "other://z/a"},
		{"https passes through", "https://example.com/a", "https://example.com/a"},
	}
	for _, c := range cases {
		got := w.rewriteSchemeURL(c.in)
		if got != c.want {
			t.Errorf("%s: rewriteSchemeURL(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// With no scheme handlers registered, URLs are never rewritten.
func TestRewriteSchemeURLNoHandlers(t *testing.T) {
	w := &webview{}
	in := "app://home/index.html"
	got := w.rewriteSchemeURL(in)
	if got != in {
		t.Errorf("rewriteSchemeURL(%q) with no handlers = %q, want unchanged", in, got)
	}
}

// canonicalSchemeURL turns the internal vhost URL back into the scheme:// form,
// restoring the authority the app navigated with so a handler sees the same URL
// as it does on macOS/Linux.
func TestCanonicalSchemeURL(t *testing.T) {
	w := &webview{
		schemeHandlers:  map[string]SchemeHandler{"app": nil},
		schemeAuthority: map[string]string{"app": "home"},
	}
	cases := []struct {
		name, in, want string
	}{
		{"restores authority", "https://app.localhost/index.html", "app://home/index.html"},
		{"restores authority + query", "https://app.localhost/x?y=1", "app://home/x?y=1"},
		{"root", "https://app.localhost/", "app://home/"},
	}
	for _, c := range cases {
		got := w.canonicalSchemeURL("app", c.in)
		if got != c.want {
			t.Errorf("%s: canonicalSchemeURL(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// Without a recorded authority, canonicalSchemeURL falls back to the scheme
// name so the URL is still well-formed scheme:// (not the internal vhost).
func TestCanonicalSchemeURLFallback(t *testing.T) {
	w := &webview{schemeHandlers: map[string]SchemeHandler{"app": nil}}
	got := w.canonicalSchemeURL("app", "https://app.localhost/index.html")
	if want := "app://app/index.html"; got != want {
		t.Errorf("canonicalSchemeURL fallback = %q, want %q", got, want)
	}
}

// Navigate rewrite followed by the request-time reconstruction round-trips the
// authority the app used, so the handler URL matches the original scheme:// URL.
func TestSchemeURLRoundTrip(t *testing.T) {
	w := &webview{
		schemeHandlers:  map[string]SchemeHandler{"app": nil},
		schemeAuthority: map[string]string{},
	}
	// The app navigates here; rewriteSchemeURL records the "home" authority.
	w.rewriteSchemeURL("app://home/index.html")
	// A sub-resource request arrives on the vhost origin and is reconstructed.
	got := w.canonicalSchemeURL("app", "https://app.localhost/assets/app.js")
	if want := "app://home/assets/app.js"; got != want {
		t.Errorf("round-trip = %q, want %q", got, want)
	}
}

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

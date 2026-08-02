// Package editor ships a small, dependency-free code editor for glaze
// WebViews: line numbers, syntax highlighting, autocompletion, error marks and
// bracket matching, in plain embedded HTML/CSS/JS — no CDN, no framework,
// nothing fetched at runtime, so a single-binary app stays a single binary.
//
// The page side is one JS class, GlazeEditor (see editor.js for its API), plus
// pluggable language definitions; Filo and SQL ship in lang/. The Go side only
// hands the app the assets to serve or inline:
//
//	js, _ := editor.JS("filo")             // core + the filo definition
//	css := editor.CSS()
//	mux.Handle("/editor.js", ...)          // or inline in the page
//
// An app then instantiates it from its own page:
//
//	<div id="ed" style="height: 20em"></div>
//	<script>
//	  const ed = new GlazeEditor(document.getElementById("ed"),
//	    {language: "filo", completions: ["self-x", "seek"]});
//	</script>
package editor

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed editor.js editor.css lang/*.js
var assets embed.FS

// CSS is the editor's stylesheet. Theme it by overriding the CSS variables
// declared on .ge (see editor.css).
func CSS() []byte {
	b, err := assets.ReadFile("editor.css")
	if err != nil {
		panic("editor: embedded editor.css missing: " + err.Error()) // unreachable: compiled in
	}
	return b
}

// JS returns the editor core followed by the requested language definitions,
// ready to serve or inline as one script. Asking for no languages is valid —
// an app can register its own on the page — and asking for one that does not
// exist is an error rather than a silently colorless editor.
func JS(langs ...string) ([]byte, error) {
	var buf bytes.Buffer
	core, err := assets.ReadFile("editor.js")
	if err != nil {
		panic("editor: embedded editor.js missing: " + err.Error()) // unreachable: compiled in
	}
	buf.Write(core)
	for _, l := range langs {
		b, err := assets.ReadFile("lang/" + l + ".js")
		if err != nil {
			return nil, fmt.Errorf("editor: unknown language %q (have %s)", l, strings.Join(Languages(), ", "))
		}
		buf.WriteByte('\n')
		buf.Write(b)
	}
	return buf.Bytes(), nil
}

// Languages lists the language definitions shipped in this package.
func Languages() []string {
	entries, err := fs.ReadDir(assets, "lang")
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		out = append(out, strings.TrimSuffix(e.Name(), ".js"))
	}
	sort.Strings(out)
	return out
}

package editor

import (
	"strings"
	"testing"
)

func TestJSBundlesCoreAndLanguages(t *testing.T) {
	core, err := JS()
	if err != nil {
		t.Fatalf("JS(): %v", err)
	}
	if !strings.Contains(string(core), "class GlazeEditor") {
		t.Fatal("the core bundle does not define GlazeEditor")
	}

	both, err := JS("filo", "sql")
	if err != nil {
		t.Fatalf("JS(filo, sql): %v", err)
	}
	for _, want := range []string{"languages.filo", "languages.sql"} {
		if !strings.Contains(string(both), want) {
			t.Errorf("bundle is missing %s", want)
		}
	}
	if len(both) <= len(core) {
		t.Error("adding languages did not grow the bundle")
	}
}

func TestUnknownLanguageIsAnError(t *testing.T) {
	_, err := JS("brainfuck")
	if err == nil {
		t.Fatal("an unknown language must be an error, not a colorless editor")
	}
	if !strings.Contains(err.Error(), "filo") {
		t.Errorf("the error should list what exists, got: %v", err)
	}
}

func TestShippedLanguages(t *testing.T) {
	langs := Languages()
	want := map[string]bool{"filo": true, "sql": true}
	for _, l := range langs {
		delete(want, l)
	}
	if len(want) != 0 {
		t.Errorf("missing shipped languages: %v (got %v)", want, langs)
	}
}

func TestCSSDeclaresTheThemeVariables(t *testing.T) {
	css := string(CSS())
	// The variables are the theming contract: an app recolors the editor by
	// overriding them, so renaming one is an API break this test makes loud.
	for _, v := range []string{"--ge-bg", "--ge-fg", "--ge-caret", "--ge-t-k", "--ge-t-s", "--ge-t-c"} {
		if !strings.Contains(css, v+":") {
			t.Errorf("editor.css no longer declares %s", v)
		}
	}
}

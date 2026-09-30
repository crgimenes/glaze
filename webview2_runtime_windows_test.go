package glaze

import (
	"os"
	"path/filepath"
	"testing"
)

// A self-update can leave the registration pointing at a deleted version folder
// while a newer runtime sits beside it; findEmbeddedBrowserDLL then falls back
// to newestRuntimeDLL on the registered folder's parent.
func TestNewestRuntimeDLL(t *testing.T) {
	dir := t.TempDir()
	mk := func(version string, withDLL bool) {
		d := filepath.Join(dir, version, "EBWebView", arch())
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if withDLL {
			if err := os.WriteFile(filepath.Join(d, "EmbeddedBrowserWebView.dll"), nil, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("99.0.1000.1", true)   // build below minAPIVersion: ignored
	mk("154.0.4258.37", true) // the runtime a self-update left behind
	mk("200.0.9999.1", false) // newer folder without the DLL: ignored
	mk("154.0.4258.9", true)  // older build of the same major: loses
	if err := os.WriteFile(filepath.Join(dir, "SetupMetrics"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	got, ok := newestRuntimeDLL(dir)
	want := filepath.Join(dir, "154.0.4258.37", "EBWebView", arch(), "EmbeddedBrowserWebView.dll")
	if !ok || got != want {
		t.Fatalf("newestRuntimeDLL = %q, %v; want %q, true", got, ok, want)
	}

	if _, ok := newestRuntimeDLL(filepath.Join(dir, "missing")); ok {
		t.Fatal("newestRuntimeDLL on a missing dir reported a runtime")
	}
}

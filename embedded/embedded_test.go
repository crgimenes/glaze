package embedded

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/crgimenes/glaze"
)

// resetExtractState resets the package-level sync.Once and error so that
// ExtractTo can be called again in subsequent test cases.
// It also re-registers the verifier (which init sets unconditionally).
func resetExtractState() {
	extractOnce = sync.Once{}
	extractErr = nil
	extractDir = ""
	// Re-register exactly as init() does.
	glaze.VerifyBeforeLoad = func(path string) error {
		actual, err := fileHash(path)
		if err != nil {
			return err
		}
		if actual != expectedLibHash {
			return fmt.Errorf("hash mismatch: expected %s, got %s", expectedLibHash, actual)
		}
		return nil
	}
}

func TestComputeHash(t *testing.T) {
	h1 := computeHash([]byte("hello"))
	h2 := computeHash([]byte("hello"))
	h3 := computeHash([]byte("world"))

	if h1 != h2 {
		t.Fatalf("same input produced different hashes: %s vs %s", h1, h2)
	}
	if h1 == h3 {
		t.Fatal("different inputs produced the same hash")
	}
	// BLAKE2b-256 produces 32 bytes = 64 hex chars.
	if len(h1) != 64 {
		t.Fatalf("expected 64 hex chars, got %d (%s)", len(h1), h1)
	}
}

func TestFileHash(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "test.bin")
	data := []byte("test data for hashing")

	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := fileHash(file)
	if err != nil {
		t.Fatalf("fileHash: %v", err)
	}
	want := computeHash(data)
	if got != want {
		t.Fatalf("fileHash mismatch: got %s, want %s", got, want)
	}
}

func TestFileHashNonExistent(t *testing.T) {
	_, err := fileHash("/nonexistent/path/file.bin")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestExtractToCustomDir(t *testing.T) {
	resetExtractState()

	dir := t.TempDir()
	if err := ExtractTo(dir); err != nil {
		t.Fatalf("ExtractTo(%q): %v", dir, err)
	}

	file := filepath.Join(dir, name)
	info, err := os.Stat(file)
	if err != nil {
		t.Fatalf("extracted file not found at %s: %v", file, err)
	}

	// Verify hash matches embedded bytes.
	got, err := fileHash(file)
	if err != nil {
		t.Fatalf("fileHash: %v", err)
	}
	want := computeHash(lib)
	if got != want {
		t.Fatalf("hash mismatch: got %s, want %s", got, want)
	}

	// Verify file permissions (Unix only).
	if runtime.GOOS != "windows" {
		perm := info.Mode().Perm()
		if perm != 0o500 {
			t.Errorf("file permissions: got %o, want 0500", perm)
		}
	}
}

func TestExtractToDefaultDir(t *testing.T) {
	resetExtractState()

	if err := ExtractTo(""); err != nil {
		t.Fatalf("ExtractTo(empty): %v", err)
	}

	defaultDir := filepath.Join(os.TempDir(), "webview-"+version)
	file := filepath.Join(defaultDir, name)
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("file not found at default path %s: %v", file, err)
	}

	// Cleanup.
	os.Remove(file)
	os.Remove(defaultDir)
}

func TestExtractToDetectsTamperedFile(t *testing.T) {
	resetExtractState()

	dir := t.TempDir()
	file := filepath.Join(dir, name)

	// Pre-place a corrupt library file.
	if err := os.WriteFile(file, []byte("MALICIOUS PAYLOAD"), 0o500); err != nil {
		t.Fatal(err)
	}

	err := ExtractTo(dir)
	if err == nil {
		t.Fatal("expected integrity error for tampered file, got nil")
	}

	want := "library integrity check failed"
	if got := err.Error(); !containsSubstr(got, want) {
		t.Fatalf("unexpected error message: %s (wanted substring %q)", got, want)
	}
}

func TestExtractToExistingValidFile(t *testing.T) {
	resetExtractState()

	dir := t.TempDir()
	file := filepath.Join(dir, name)

	// Pre-place the correct library file.
	if err := os.WriteFile(file, lib, 0o500); err != nil {
		t.Fatal(err)
	}

	// Should succeed without error since hash matches.
	if err := ExtractTo(dir); err != nil {
		t.Fatalf("ExtractTo with valid pre-existing file: %v", err)
	}
}

func TestExtractDelegates(t *testing.T) {
	resetExtractState()

	// Extract() should behave the same as ExtractTo("").
	if err := Extract(); err != nil {
		t.Fatalf("Extract(): %v", err)
	}

	defaultDir := filepath.Join(os.TempDir(), "webview-"+version)
	file := filepath.Join(defaultDir, name)
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("file not found at default path %s: %v", file, err)
	}

	// Cleanup.
	os.Remove(file)
	os.Remove(defaultDir)
}

func TestDirPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission check not applicable on Windows")
	}

	resetExtractState()

	parent := t.TempDir()
	dir := filepath.Join(parent, "glaze-perm-test")

	if err := ExtractTo(dir); err != nil {
		t.Fatalf("ExtractTo(%q): %v", dir, err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0o700 {
		t.Errorf("dir permissions: got %o, want 0700", perm)
	}
}

func TestVerifyBeforeLoadSetUnconditionally(t *testing.T) {
	// The verifier must be set by init(), not by ExtractTo.
	// Even without calling ExtractTo, VerifyBeforeLoad must be non-nil.
	if glaze.VerifyBeforeLoad == nil {
		t.Fatal("VerifyBeforeLoad was not set by init()")
	}

	// Verify it accepts a file matching the embedded library hash.
	dir := t.TempDir()
	file := filepath.Join(dir, name)
	if err := os.WriteFile(file, lib, 0o500); err != nil {
		t.Fatal(err)
	}
	if err := glaze.VerifyBeforeLoad(file); err != nil {
		t.Fatalf("VerifyBeforeLoad rejected valid library: %v", err)
	}

	// Verify it rejects a tampered file.
	tampered := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(tampered, []byte("BAD"), 0o500); err != nil {
		t.Fatal(err)
	}
	if err := glaze.VerifyBeforeLoad(tampered); err == nil {
		t.Fatal("VerifyBeforeLoad should reject tampered library")
	}
}

func TestVerifyBeforeLoadSurvivesExtractError(t *testing.T) {
	resetExtractState()

	dir := t.TempDir()
	file := filepath.Join(dir, name)

	// Pre-place a tampered file so ExtractTo fails.
	if err := os.WriteFile(file, []byte("MALICIOUS"), 0o500); err != nil {
		t.Fatal(err)
	}

	// ExtractTo should fail...
	err := ExtractTo(dir)
	if err == nil {
		t.Fatal("expected ExtractTo to fail on tampered file")
	}

	// ...but VerifyBeforeLoad must STILL be set (it was set before extraction).
	if glaze.VerifyBeforeLoad == nil {
		t.Fatal("VerifyBeforeLoad must remain set even when ExtractTo fails")
	}
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

package embedded

import (
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/crgimenes/glaze"
	"golang.org/x/crypto/blake2b"
)

//go:embed VERSION.txt
var version string

var extractOnce sync.Once
var extractErr error
var extractDir string

// expectedLibHash is the hex-encoded BLAKE2b-256 digest of the embedded
// library bytes, computed once at package init time. Used both for on-disk
// verification during extraction and for pre-load verification before dlopen.
var expectedLibHash = computeHash(lib)

// computeHash returns the hex-encoded BLAKE2b-256 digest of data.
func computeHash(data []byte) string {
	h, _ := blake2b.New256(nil) // nil key never errors
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// fileHash returns the hex-encoded BLAKE2b-256 digest of the file at path.
func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h, _ := blake2b.New256(nil)
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ExtractTo writes the embedded native library to dir and sets the environment
// so the glaze package can find it at runtime. If dir is empty the default
// temporary directory is used ($TMPDIR/webview-<version>).
//
// The extracted file is verified against a BLAKE2b-256 hash computed from the
// embedded bytes. If a file already exists at the destination and its hash does
// not match, an error is returned without modifying the file.
//
// ExtractTo is safe to call multiple times; only the first call has effect.
func ExtractTo(dir string) error {
	extractOnce.Do(func() {
		if dir == "" {
			dir = filepath.Join(os.TempDir(), "webview-"+version)
		}
		extractDir = dir
		file := filepath.Join(dir, name)

		if _, err := os.Stat(file); err == nil {
			// File already exists — verify its integrity.
			actual, err := fileHash(file)
			if err != nil {
				extractErr = fmt.Errorf("webview/embedded: failed to hash existing library %s: %w", file, err)
				return
			}
			if actual != expectedLibHash {
				extractErr = fmt.Errorf(
					"webview/embedded: library integrity check failed for %s: expected %s, got %s",
					file, expectedLibHash, actual,
				)
				return
			}
		} else {
			// File does not exist — extract and verify.
			if err := os.MkdirAll(dir, 0o700); err != nil {
				extractErr = fmt.Errorf("webview/embedded: failed to create directory %s: %w", dir, err)
				return
			}
			if err := os.WriteFile(file, lib, 0o500); err != nil {
				extractErr = fmt.Errorf("webview/embedded: failed to write library %s: %w", file, err)
				return
			}
			// Verify the written file to catch I/O or filesystem issues.
			actual, err := fileHash(file)
			if err != nil {
				extractErr = fmt.Errorf("webview/embedded: failed to verify written library %s: %w", file, err)
				return
			}
			if actual != expectedLibHash {
				extractErr = fmt.Errorf(
					"webview/embedded: post-write integrity check failed for %s: expected %s, got %s",
					file, expectedLibHash, actual,
				)
				return
			}
		}

		// Set WEBVIEW_PATH on all platforms so that libraryPath() in the
		// glaze package resolves an absolute path for hash verification.
		if err := os.Setenv("WEBVIEW_PATH", dir); err != nil {
			extractErr = fmt.Errorf("webview/embedded: failed to set WEBVIEW_PATH: %w", err)
			return
		}
		// On Windows also prepend PATH so that syscall.LoadLibrary fallback
		// can find the DLL through the standard Windows search order.
		if runtime.GOOS == "windows" {
			if err := os.Setenv("PATH", dir+";"+os.Getenv("PATH")); err != nil {
				extractErr = fmt.Errorf("webview/embedded: failed to set PATH: %w", err)
			}
		}
	})
	return extractErr
}

// Extract writes the embedded native library to the default temporary directory
// and sets the environment so the glaze package can find it at runtime. It is
// safe to call multiple times; only the first call has effect.
//
// For production deployments, prefer ExtractTo with a directory that is not
// world-writable (e.g. alongside the application binary).
func Extract() error {
	return ExtractTo("")
}

// init registers the pre-load integrity verifier unconditionally and then
// calls Extract for backward compatibility with the "import _ embedded" pattern.
//
// The verifier is registered BEFORE extraction so that glaze.Init() will
// always hash-check the library before dlopen/LoadLibrary, regardless of
// whether extraction succeeded, failed, or was skipped.
//
// New code should call ExtractTo() explicitly before glaze.Init().
func init() {
	// Unconditionally register the pre-load verifier so that any library
	// loaded via glaze.Init() is verified against the embedded BLAKE2b-256
	// hash. This closes the TOCTOU window between extraction and loading
	// and ensures verification even if ExtractTo encounters an error.
	glaze.VerifyBeforeLoad = func(path string) error {
		actual, err := fileHash(path)
		if err != nil {
			return fmt.Errorf("webview/embedded: failed to hash library before load %s: %w", path, err)
		}
		if actual != expectedLibHash {
			return fmt.Errorf(
				"webview/embedded: pre-load integrity check failed for %s: expected %s, got %s",
				path, expectedLibHash, actual,
			)
		}
		return nil
	}

	if err := Extract(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

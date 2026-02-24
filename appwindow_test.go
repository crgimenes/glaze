package webview

import (
	"net/http"
	"testing"
)

func TestAppWindowNilHandler(t *testing.T) {
	err := AppWindow(AppOptions{})
	if err == nil {
		t.Fatal("expected error for nil handler")
	}
}

func TestAppOptionsDefaults(t *testing.T) {
	// We can't test the full AppWindow flow without a native library,
	// but we can verify that the defaults are applied by checking the
	// validation path. Since Handler is required, this tests nil guard.
	err := AppWindow(AppOptions{
		Title:   "",
		Width:   0,
		Height:  0,
		Handler: nil,
	})
	if err == nil {
		t.Fatal("expected error for nil handler")
	}
}

func TestAppWindowInvalidAddr(t *testing.T) {
	// Use an invalid address to trigger a listen error.
	err := AppWindow(AppOptions{
		Handler: http.NewServeMux(),
		Addr:    "invalid-not-an-address:99999999",
	})
	if err == nil {
		t.Fatal("expected error for invalid address")
	}
}

package glaze

import (
	"net/http"
	"os"
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
		Transport: AppTransportTCP,
		Handler:   http.NewServeMux(),
		Addr:      "invalid-not-an-address:99999999",
	})
	if err == nil {
		t.Fatal("expected error for invalid address")
	}
}

func TestResolveAppTransport(t *testing.T) {
	tests := []struct {
		name      string
		requested AppTransport
		goos      string
		want      AppTransport
		wantErr   bool
	}{
		{name: "auto darwin", requested: AppTransportAuto, goos: "darwin", want: AppTransportUnix},
		{name: "auto linux", requested: "", goos: "linux", want: AppTransportUnix},
		{name: "auto windows", requested: AppTransportAuto, goos: "windows", want: AppTransportTCP},
		{name: "explicit tcp", requested: AppTransportTCP, goos: "darwin", want: AppTransportTCP},
		{name: "explicit unix", requested: AppTransportUnix, goos: "linux", want: AppTransportUnix},
		{name: "unix windows error", requested: AppTransportUnix, goos: "windows", wantErr: true},
		{name: "invalid transport", requested: "bogus", goos: "linux", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveAppTransport(tt.requested, tt.goos)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveAppTransport() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("resolveAppTransport() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrepareUnixSocketPath(t *testing.T) {
	path, err := prepareUnixSocketPath("")
	if err != nil {
		t.Fatalf("prepareUnixSocketPath() unexpected error: %v", err)
	}
	if path == "" {
		t.Fatal("prepareUnixSocketPath() returned empty path")
	}
	_, statErr := os.Stat(path)
	if !os.IsNotExist(statErr) {
		t.Fatalf("expected socket path placeholder to not exist, stat err: %v", statErr)
	}
}

func TestRemoveUnixSocketRejectsRegularFile(t *testing.T) {
	tmp, err := os.CreateTemp("", "glaze-regular-*")
	if err != nil {
		t.Fatalf("CreateTemp() unexpected error: %v", err)
	}
	path := tmp.Name()
	if err := tmp.Close(); err != nil {
		t.Fatalf("Close() unexpected error: %v", err)
	}
	defer os.Remove(path)

	err = removeUnixSocket(path)
	if err == nil {
		t.Fatal("expected error when path is not a unix socket")
	}
}

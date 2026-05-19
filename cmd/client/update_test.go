// cmd/client/update_test.go
package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsNewer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{name: "newer patch", current: "v1.2.3-0-gabc", latest: "v1.2.4-0-gdef", want: true},
		{name: "newer minor", current: "v1.2.3-0-gabc", latest: "v1.3.0-0-gdef", want: true},
		{name: "same version", current: "v1.2.3-0-gabc", latest: "v1.2.3-0-gabc", want: false},
		{name: "older latest", current: "v1.2.4-0-gabc", latest: "v1.2.3-0-gdef", want: false},
		{name: "current is dev", current: "dev", latest: "v1.2.3-0-gabc", want: false},
		{name: "latest is dev", current: "v1.2.3-0-gabc", latest: "dev", want: false},
		{name: "both dev", current: "dev", latest: "dev", want: false},
		{name: "plain semver current", current: "v1.2.3", latest: "v1.2.4", want: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isNewer(tt.current, tt.latest)
			if got != tt.want {
				t.Errorf("isNewer(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

func TestIsUpdateCheckDue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		lastCheck time.Time
		want      bool
	}{
		{name: "zero time (never checked)", lastCheck: time.Time{}, want: true},
		{name: "25 hours ago", lastCheck: time.Now().Add(-25 * time.Hour), want: true},
		{name: "23 hours ago", lastCheck: time.Now().Add(-23 * time.Hour), want: false},
		{name: "just now", lastCheck: time.Now(), want: false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isUpdateCheckDue(tt.lastCheck)
			if got != tt.want {
				t.Errorf("isUpdateCheckDue(%v) = %v, want %v", tt.lastCheck, got, tt.want)
			}
		})
	}
}

func TestFetchLatestVersion(t *testing.T) {
	t.Parallel()

	t.Run("returns trimmed version string", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("v1.2.3-0-gabcdef1\n"))
		}))
		defer srv.Close()

		got, err := fetchLatestVersion(srv.URL)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "v1.2.3-0-gabcdef1" {
			t.Errorf("fetchLatestVersion() = %q, want %q", got, "v1.2.3-0-gabcdef1")
		}
	})

	t.Run("returns error on non-200", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		_, err := fetchLatestVersion(srv.URL)
		if err == nil {
			t.Fatal("expected error for non-200 response")
		}
	})

	t.Run("returns error when server unreachable", func(t *testing.T) {
		t.Parallel()
		_, err := fetchLatestVersion("http://127.0.0.1:1") // nothing listening
		if err == nil {
			t.Fatal("expected error for unreachable server")
		}
	})
}

func TestInstallBinary(t *testing.T) {
	t.Parallel()

	t.Run("replaces target with source content", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		src := filepath.Join(dir, "new-binary")
		dst := filepath.Join(dir, "fyra")

		if err := os.WriteFile(src, []byte("new"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, []byte("old"), 0755); err != nil {
			t.Fatal(err)
		}

		if err := installBinary(src, dst); err != nil {
			t.Fatalf("installBinary() error = %v", err)
		}

		got, err := os.ReadFile(dst)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "new" {
			t.Errorf("dst content = %q, want %q", string(got), "new")
		}
		if _, err := os.Stat(src); !os.IsNotExist(err) {
			t.Error("expected src to be removed after rename")
		}
	})

	t.Run("returns error with clear message when dst dir is not writable", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("running as root; permission checks do not apply")
		}
		t.Parallel()
		dir := t.TempDir()
		src := filepath.Join(dir, "new-binary")
		// Create a read-only subdirectory as the install target dir
		readonlyDir := filepath.Join(dir, "readonly")
		if err := os.Mkdir(readonlyDir, 0555); err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(readonlyDir, "fyra")

		if err := os.WriteFile(src, []byte("new"), 0755); err != nil {
			t.Fatal(err)
		}

		err := installBinary(src, dst)
		if err == nil {
			t.Fatal("expected error for read-only directory")
		}
		errMsg := err.Error()
		if !strings.Contains(errMsg, "cannot update") {
			t.Errorf("error message %q missing 'cannot update'", errMsg)
		}
		if !strings.Contains(errMsg, "Re-run as root") {
			t.Errorf("error message %q missing 'Re-run as root'", errMsg)
		}
	})
}

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gitignore "github.com/sabhiram/go-gitignore"
)

func TestComputeDiff(t *testing.T) {
	serverManifest := map[string]string{
		"index.html": "abc123",
		"style.css":  "def456",
		"old.js":     "ghi789",
		"about.html": "jkl012",
	}

	localFiles := map[string]string{
		"index.html": "abc123", // unchanged
		"style.css":  "xyz999", // modified
		"new.html":   "aaa111", // new file
		"about.html": "jkl012", // unchanged
	}

	toUpload, toDelete := computeDiff(localFiles, serverManifest)

	// Should upload modified and new files.
	if len(toUpload) != 2 {
		t.Fatalf("expected 2 files to upload, got %d", len(toUpload))
	}
	uploadPaths := map[string]bool{}
	for _, f := range toUpload {
		uploadPaths[f] = true
	}
	if !uploadPaths["style.css"] {
		t.Error("expected style.css in upload set (modified)")
	}
	if !uploadPaths["new.html"] {
		t.Error("expected new.html in upload set (new)")
	}

	// Should delete files on server but not local.
	if len(toDelete) != 1 {
		t.Fatalf("expected 1 file to delete, got %d", len(toDelete))
	}
	if toDelete[0] != "old.js" {
		t.Errorf("expected old.js to delete, got %s", toDelete[0])
	}
}

func TestComputeDiffEmptyServer(t *testing.T) {
	localFiles := map[string]string{
		"index.html": "abc123",
		"style.css":  "def456",
	}

	toUpload, toDelete := computeDiff(localFiles, nil)

	if len(toUpload) != 2 {
		t.Fatalf("expected 2 files to upload, got %d", len(toUpload))
	}
	if len(toDelete) != 0 {
		t.Fatalf("expected 0 files to delete, got %d", len(toDelete))
	}
}

func TestComputeDiffAllUnchanged(t *testing.T) {
	serverManifest := map[string]string{
		"index.html": "abc123",
	}
	localFiles := map[string]string{
		"index.html": "abc123",
	}

	toUpload, toDelete := computeDiff(localFiles, serverManifest)

	if len(toUpload) != 0 {
		t.Fatalf("expected 0 files to upload, got %d", len(toUpload))
	}
	if len(toDelete) != 0 {
		t.Fatalf("expected 0 files to delete, got %d", len(toDelete))
	}
}

func TestTarballDiffDirOnlyContainsSpecifiedFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/keep.txt", []byte("hello"), 0644)
	os.WriteFile(dir+"/skip.txt", []byte("world"), 0644)
	os.MkdirAll(dir+"/sub", 0755)
	os.WriteFile(dir+"/sub/nested.txt", []byte("nested"), 0644)

	uploadSet := map[string]bool{
		"keep.txt":       true,
		"sub/nested.txt": true,
	}

	var buf bytes.Buffer
	err := tarballDiffDir(dir, &buf, uploadSet)
	if err != nil {
		t.Fatal(err)
	}

	// Read back the tarball and verify contents.
	gr, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)

	var found []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		found = append(found, hdr.Name)
	}

	if len(found) != 2 {
		t.Fatalf("expected 2 files in tarball, got %d: %v", len(found), found)
	}
	for _, name := range found {
		if !uploadSet[name] {
			t.Errorf("unexpected file in tarball: %s", name)
		}
	}
}

func TestIsSecretFile(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{".env", true},
		{".env.production", true},
		{".envrc", true},
		{"server.pem", true},
		{"tls.key", true},
		{"cert.p12", true},
		{"cert.pfx", true},
		{".netrc", true},
		{"index.html", false},
		{"main.go", false},
		{"keys.txt", false},
		{"README.md", false},
	}
	for _, c := range cases {
		if got := isSecretFile(c.name); got != c.want {
			t.Errorf("isSecretFile(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSkippedSecretFiles(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir, "index.html", "<html>")
	mustWrite(t, dir, ".env", "SECRET=1")
	mustWrite(t, dir, "sub/tls.key", "-----BEGIN-----")
	mustWrite(t, dir, "ignored.key", "should be filtered by ignore rule")
	// Secrets inside pruned directories must not be reported.
	mustWrite(t, dir, ".git/config.key", "x")
	mustWrite(t, dir, "node_modules/dep/.env", "x")

	ign := gitignore.CompileIgnoreLines("ignored.key")
	got := skippedSecretFiles(dir, ign)

	want := []string{".env", "sub/tls.key"}
	if len(got) != len(want) {
		t.Fatalf("skippedSecretFiles = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("skippedSecretFiles[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestWarnSkippedSecrets(t *testing.T) {
	var buf bytes.Buffer
	warnSkippedSecrets(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("empty list should produce no output, got %q", buf.String())
	}

	buf.Reset()
	warnSkippedSecrets(&buf, []string{".env"})
	out := buf.String()
	if !strings.Contains(out, "Skipped 1 secret file") || !strings.Contains(out, ".env") {
		t.Errorf("single-file notice missing expected text: %q", out)
	}

	buf.Reset()
	warnSkippedSecrets(&buf, []string{".env", "tls.key"})
	out = buf.String()
	if !strings.Contains(out, "Skipped 2 secret files") {
		t.Errorf("plural notice missing expected text: %q", out)
	}
}

// mustWrite creates dir/rel (with parent dirs) containing data, failing the test on error.
func mustWrite(t *testing.T, dir, rel, data string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}

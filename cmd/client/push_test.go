package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"testing"
)

func TestComputeDiff(t *testing.T) {
	serverManifest := map[string]string{
		"index.html":  "abc123",
		"style.css":   "def456",
		"old.js":      "ghi789",
		"about.html":  "jkl012",
	}

	localFiles := map[string]string{
		"index.html":  "abc123",  // unchanged
		"style.css":   "xyz999",  // modified
		"new.html":    "aaa111",  // new file
		"about.html":  "jkl012",  // unchanged
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
		"keep.txt":      true,
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

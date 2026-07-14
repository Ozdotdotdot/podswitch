package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestApplyVerifiesAndReplacesBothBinaries(t *testing.T) {
	archive := releaseArchive(t, "podswitch_linux_amd64", map[string]string{
		"podswitchd": "new daemon",
		"podswitch":  "new cli",
	})
	checksum := fmt.Sprintf("%x  dist/podswitch_linux_amd64.tar.gz\n", sha256.Sum256(archive))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/checksums.txt":
			_, _ = w.Write([]byte(checksum))
		case "/podswitch_linux_amd64.tar.gz":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "podswitchd"), []byte("old daemon"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Apply(context.Background(), Config{
		BaseURL: server.URL,
		BinDir:  dir,
		GOOS:    "linux",
		GOARCH:  "amd64",
		Client:  server.Client(),
	}); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"podswitchd": "new daemon", "podswitch": "new cli"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Errorf("%s mode = %o, want 755", name, info.Mode().Perm())
		}
	}
}

func TestApplyRejectsBadChecksumBeforeReplacingFiles(t *testing.T) {
	archive := releaseArchive(t, "podswitch_linux_amd64", map[string]string{"podswitchd": "new", "podswitch": "new"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/checksums.txt" {
			_, _ = w.Write([]byte("00  dist/podswitch_linux_amd64.tar.gz\n"))
			return
		}
		_, _ = w.Write(archive)
	}))
	defer server.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "podswitchd"), []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := Apply(context.Background(), Config{BaseURL: server.URL, BinDir: dir, GOOS: "linux", GOARCH: "amd64", Client: server.Client()})
	if err == nil {
		t.Fatal("Apply accepted a bad checksum")
	}
	got, readErr := os.ReadFile(filepath.Join(dir, "podswitchd"))
	if readErr != nil || string(got) != "old" {
		t.Fatalf("bad archive changed podswitchd: %q, %v", got, readErr)
	}
}

func releaseArchive(t *testing.T, root string, files map[string]string) []byte {
	t.Helper()
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		data := []byte(content)
		if err := tw.WriteHeader(&tar.Header{Name: root + "/" + name, Mode: 0o755, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

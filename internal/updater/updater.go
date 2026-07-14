// Package updater installs the latest prebuilt podswitch release beside the
// currently running binary. It is intentionally local only: transport to a
// machine is the existing user's shell/session, while service restart is
// handled by cmd/podswitchd after the verified binaries are in place.
package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

const LatestReleaseURL = "https://github.com/Ozdotdotdot/podswitch/releases/latest/download"

// Config describes the release and destination for one update. BaseURL is
// exposed so the downloader can be tested against a local release fixture.
type Config struct {
	BaseURL string
	BinDir  string
	GOOS    string
	GOARCH  string
	Client  *http.Client
}

// Latest updates the sibling podswitch and podswitchd binaries in binDir for
// the architecture of the machine running this process.
func Latest(ctx context.Context, binDir string) error {
	return Apply(ctx, Config{
		BaseURL: LatestReleaseURL,
		BinDir:  binDir,
		GOOS:    runtime.GOOS,
		GOARCH:  runtime.GOARCH,
		Client:  http.DefaultClient,
	})
}

// Apply downloads an archive and its release checksum, verifies it, then
// atomically replaces both executable files in BinDir.
func Apply(ctx context.Context, cfg Config) error {
	if cfg.GOOS != "linux" || (cfg.GOARCH != "amd64" && cfg.GOARCH != "arm64") {
		return fmt.Errorf("no prebuilt podswitch release for %s/%s", cfg.GOOS, cfg.GOARCH)
	}
	if cfg.BinDir == "" {
		return fmt.Errorf("empty binary directory")
	}
	if cfg.Client == nil {
		cfg.Client = http.DefaultClient
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	asset := fmt.Sprintf("podswitch_%s_%s.tar.gz", cfg.GOOS, cfg.GOARCH)
	archive, err := download(ctx, cfg.Client, base+"/"+asset)
	if err != nil {
		return err
	}
	checksums, err := download(ctx, cfg.Client, base+"/checksums.txt")
	if err != nil {
		return err
	}
	if err := verifyChecksum(asset, archive, checksums); err != nil {
		return err
	}
	binaries, err := unpack(asset, archive)
	if err != nil {
		return err
	}
	for _, name := range []string{"podswitchd", "podswitch"} {
		if err := writeExecutable(cfg.BinDir, name, binaries[name]); err != nil {
			return err
		}
	}
	return nil
}

func download(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	return data, nil
}

func verifyChecksum(asset string, archive, checksums []byte) error {
	want := ""
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && path.Base(fields[len(fields)-1]) == asset {
			want = fields[0]
			break
		}
	}
	if want == "" {
		return fmt.Errorf("checksums.txt has no checksum for %s", asset)
	}
	got := fmt.Sprintf("%x", sha256.Sum256(archive))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("checksum mismatch for %s", asset)
	}
	return nil
}

func unpack(asset string, archive []byte) (map[string][]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", asset, err)
	}
	defer gz.Close()
	wantPrefix := strings.TrimSuffix(asset, ".tar.gz") + "/"
	tr := tar.NewReader(gz)
	result := make(map[string][]byte, 2)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", asset, err)
		}
		if hdr.Typeflag != tar.TypeReg || !strings.HasPrefix(hdr.Name, wantPrefix) {
			continue
		}
		name := strings.TrimPrefix(hdr.Name, wantPrefix)
		if name != "podswitchd" && name != "podswitch" {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("extract %s: %w", name, err)
		}
		result[name] = data
	}
	for _, name := range []string{"podswitchd", "podswitch"} {
		if len(result[name]) == 0 {
			return nil, fmt.Errorf("%s does not contain %s", asset, name)
		}
	}
	return result, nil
}

func writeExecutable(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+name+".update-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		return err
	}
	return nil
}

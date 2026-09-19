// Package selfupdate replaces the running devkit binary with a newer GitHub
// release (the same assets install.sh downloads).
package selfupdate

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sezznaw/devkit/internal/github"
)

const binaryName = "devkit"

type Updater struct {
	Client *github.Client
	Repo   string // owner/repo
	// Current is the running version (buildinfo.Version).
	Current string
	// Target is the binary to replace; defaults to the running executable.
	Target string
	Log    func(format string, args ...any)
}

// Latest returns the newest release tag, e.g. "v1.2.0".
func (u *Updater) Latest(ctx context.Context) (string, error) {
	rel, err := u.Client.LatestRelease(ctx, u.Repo)
	if err != nil {
		return "", err
	}
	return rel.TagName, nil
}

// Normalize strips a leading "v" so "v1.2.0" and "1.2.0" compare equal.
func Normalize(v string) string { return strings.TrimPrefix(strings.TrimSpace(v), "v") }

// Apply downloads the release for tag and swaps the current executable.
// Returns the path that was replaced.
func (u *Updater) Apply(ctx context.Context, tag string) (string, error) {
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}
	exe := u.Target
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return "", err
		}
	}
	exe, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}

	rel, err := u.Client.ReleaseByTag(ctx, u.Repo, tag)
	if err != nil {
		return "", err
	}
	archive := fmt.Sprintf("%s_%s_%s.tar.gz", binaryName, runtime.GOOS, runtime.GOARCH)
	asset, ok := rel.Asset(archive)
	if !ok {
		return "", fmt.Errorf("release %s has no asset %s (no build for this platform?)", tag, archive)
	}
	sumAsset, ok := rel.Asset("checksums.txt")
	if !ok {
		return "", fmt.Errorf("release %s has no checksums.txt", tag)
	}
	u.Log("downloading %s %s (%s/%s)", binaryName, tag, runtime.GOOS, runtime.GOARCH)

	sums, err := u.fetchChecksums(ctx, sumAsset)
	if err != nil {
		return "", err
	}
	want, ok := sums[archive]
	if !ok {
		return "", fmt.Errorf("checksums.txt has no entry for %s", archive)
	}

	body, err := u.Client.DownloadAsset(ctx, asset)
	if err != nil {
		return "", err
	}
	defer body.Close()

	tmpDir, err := os.MkdirTemp(filepath.Dir(exe), ".devkit-update-*")
	if err != nil {
		return "", fmt.Errorf("cannot write next to %s: %w (try again with sudo)", exe, err)
	}
	defer os.RemoveAll(tmpDir)

	archivePath := filepath.Join(tmpDir, archive)
	got, err := saveAndHash(body, archivePath)
	if err != nil {
		return "", err
	}
	if got != want {
		return "", fmt.Errorf("checksum mismatch for %s: got %s want %s", archive, got, want)
	}

	binPath := filepath.Join(tmpDir, binaryName)
	if err := extractBinary(archivePath, binaryName, binPath); err != nil {
		return "", err
	}
	if err := os.Chmod(binPath, 0o755); err != nil {
		return "", err
	}
	// Rename is atomic on the same filesystem; a running process keeps its old inode.
	if err := os.Rename(binPath, exe); err != nil {
		return "", fmt.Errorf("replace %s: %w (try again with sudo)", exe, err)
	}
	return exe, nil
}

func (u *Updater) fetchChecksums(ctx context.Context, a github.Asset) (map[string]string, error) {
	body, err := u.Client.DownloadAsset(ctx, a)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	sums := map[string]string{}
	sc := bufio.NewScanner(body)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 {
			sums[strings.TrimPrefix(fields[1], "*")] = fields[0]
		}
	}
	return sums, sc.Err()
}

func saveAndHash(r io.Reader, path string) (string, error) {
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extractBinary pulls a single file named name out of a tar.gz.
func extractBinary(archivePath, name, dest string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return errors.New("archive does not contain " + name)
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg || filepath.Base(hdr.Name) != name {
			continue
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, tr)
		out.Close()
		return err
	}
}

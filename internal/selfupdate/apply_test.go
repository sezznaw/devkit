package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sezznaw/devkit/internal/github"
)

func tarGz(t *testing.T, name, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	tw.Write([]byte(body))
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// fakeGitHub serves the release endpoints self-update touches. Asset URLs
// point back at the fake so DownloadAsset works unchanged.
func fakeGitHub(t *testing.T, archive []byte, checksums string) *httptest.Server {
	t.Helper()
	arch := fmt.Sprintf("devkit_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "unauthorized", 401)
			return
		}
		release := func() string {
			return fmt.Sprintf(`{"tag_name":"v1.1.0","assets":[{"name":%q,"url":"%s/assets/1"},{"name":"checksums.txt","url":"%s/assets/2"}]}`, arch, srv.URL, srv.URL)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"), strings.HasSuffix(r.URL.Path, "/releases/tags/v1.1.0"):
			fmt.Fprint(w, release())
		case r.URL.Path == "/assets/1" && r.Header.Get("Accept") == "application/octet-stream":
			w.Write(archive)
		case r.URL.Path == "/assets/2":
			fmt.Fprint(w, checksums)
		default:
			http.NotFound(w, r)
		}
	}))
	return srv
}

// enterpriseClient makes the client talk to the fake: host != github.com uses https://<host>/api/v3,
// so we strip the scheme and rely on the test server's URL (http) via a custom base. Simplest is
// to point host at the test server and override the api base by using the URL's host.
func clientFor(srv *httptest.Server) *github.Client {
	return github.NewWithBase(srv.URL, srv.URL, "tok")
}

func TestApplyReplacesBinary(t *testing.T) {
	archive := tarGz(t, "devkit", "NEW BINARY")
	sum := sha256.Sum256(archive)
	checksums := fmt.Sprintf("%s  devkit_%s_%s.tar.gz\n", hex.EncodeToString(sum[:]), runtime.GOOS, runtime.GOARCH)
	srv := fakeGitHub(t, archive, checksums)
	defer srv.Close()

	target := filepath.Join(t.TempDir(), "devkit")
	if err := os.WriteFile(target, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	u := &Updater{Client: clientFor(srv), Repo: "sezznaw/devkit", Current: "v1.0.0", Target: target, Log: func(string, ...any) {}}

	latest, err := u.Latest(context.Background())
	if err != nil || latest != "v1.1.0" {
		t.Fatalf("Latest = %q, %v", latest, err)
	}
	if _, err := u.Apply(context.Background(), latest); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "NEW BINARY" {
		t.Errorf("binary not replaced: %q", got)
	}
	info, _ := os.Stat(target)
	if info.Mode().Perm()&0o111 == 0 {
		t.Error("binary is not executable")
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(target), ".devkit-update-*")); len(left) != 0 {
		t.Errorf("temp dir left behind: %v", left)
	}
}

func TestApplyRejectsBadChecksum(t *testing.T) {
	archive := tarGz(t, "devkit", "NEW BINARY")
	checksums := fmt.Sprintf("%s  devkit_%s_%s.tar.gz\n", strings.Repeat("0", 64), runtime.GOOS, runtime.GOARCH)
	srv := fakeGitHub(t, archive, checksums)
	defer srv.Close()

	target := filepath.Join(t.TempDir(), "devkit")
	os.WriteFile(target, []byte("OLD"), 0o755)
	u := &Updater{Client: clientFor(srv), Repo: "sezznaw/devkit", Target: target, Log: func(string, ...any) {}}
	if _, err := u.Apply(context.Background(), "v1.1.0"); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum error, got %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "OLD" {
		t.Error("binary must be untouched after a failed update")
	}
}

package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sezznaw/devkit/internal/github"
)

// githubTarball mimics GET /repos/{owner}/{repo}/tarball/{ref}:
// every entry is prefixed with "<owner>-<repo>-<sha>/".
func githubTarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		hdr := &tar.Header{Name: "sezznaw-devkit-registry-abc123/" + name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestExtractComponentStripsPrefixes(t *testing.T) {
	data := githubTarball(t, map[string]string{
		"components/logger/component.json":                        `{"name":"logger","version":"0.1.0"}`,
		"components/logger/files/pkg/{{.Package}}/logger.go.tmpl": "package x",
		"components/other/component.json":                         `{"name":"other","version":"9.9.9"}`,
	})
	dest := t.TempDir()
	if err := extractComponent(bytes.NewReader(data), dest, "components/logger"); err != nil {
		t.Fatal(err)
	}
	c, err := LoadComponent(dest)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "logger" || c.Version != "0.1.0" {
		t.Errorf("unexpected component %+v", c)
	}
	if _, err := os.Stat(filepath.Join(dest, "files", "pkg", "{{.Package}}", "logger.go.tmpl")); err != nil {
		t.Error(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "..", "other")); err == nil {
		t.Error("unrelated component must not be extracted")
	}
}

func TestExtractComponentRejectsTraversal(t *testing.T) {
	data := githubTarball(t, map[string]string{"components/logger/../../evil": "x"})
	if err := extractComponent(bytes.NewReader(data), t.TempDir(), "components/logger"); err == nil {
		t.Fatal("expected traversal error")
	}
}

func TestExtractComponentEmpty(t *testing.T) {
	data := githubTarball(t, map[string]string{"components/other/component.json": "{}"})
	if err := extractComponent(bytes.NewReader(data), t.TempDir(), "components/logger"); err == nil {
		t.Fatal("expected error for empty archive")
	}
}

const testRegistryRepo = "sezznaw/devkit-registry"

// componentTarball is the registry tarball at tag <name>/v<version>.
func componentTarball(t *testing.T, name, version string) []byte {
	t.Helper()
	return githubTarball(t, map[string]string{
		"components/" + name + "/component.json":     `{"name":"` + name + `","version":"` + version + `"}`,
		"components/" + name + "/files/main.go.tmpl": "package main\n",
	})
}

// tarballServer answers GET /repos/<repo>/tarball/<name>/v<version> with
// tarball and counts the downloads. onRequest, when set, runs before the
// answer: by then Fetch has found nothing in the cache and has not renamed its
// download into place yet, the window in which another devkit process can
// fill the cache. It runs on the server's goroutine (no t.Fatal there).
func tarballServer(t *testing.T, name, version string, tarball []byte, onRequest func()) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	path := "/repos/" + testRegistryRepo + "/tarball/" + url.PathEscape(TagFor(name, version))
	downloads := new(atomic.Int32)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != path {
			http.NotFound(w, r)
			return
		}
		downloads.Add(1)
		if onRequest != nil {
			onRequest()
		}
		w.Write(tarball)
	}))
	t.Cleanup(srv.Close)
	return srv, downloads
}

// cachingSource is the GitHub source registry.Open builds, talking to srv and
// caching under cacheDir. Each devkit process has its own.
func cachingSource(srv *httptest.Server, cacheDir string) *GitHub {
	g := NewGitHub("", "", testRegistryRepo, cacheDir)
	g.client = github.NewWithBase(srv.URL, srv.URL, "")
	return g
}

// assertOnlyEntry fails unless dir holds exactly one entry, name: a download
// that lost the race (<name>-NNN beside it) must not stay behind.
func assertOnlyEntry(t *testing.T, dir, name string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	if len(got) != 1 || got[0] != name {
		t.Errorf("%s holds %v, want only %s", dir, got, name)
	}
}

// filesUnder maps every file below dir (slash-separated, relative) to its content.
func filesUnder(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		files[filepath.ToSlash(rel)] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// Several `devkit update` processes, one per service, need a template version
// nobody has cached yet: each downloads it, and every one but the first failed
// to move its download into place (2026-11-06, two services: "rename
// .../kitex-service-NNN .../kitex-service/0.78.13: file exists"). The server
// holds each download until all of them have asked, so every Fetch has found
// the cache empty before the first one renames.
func TestFetchConcurrentFirstDownloads(t *testing.T) {
	const name, version, n = "kitex-service", "0.78.13", 4
	cache := t.TempDir()
	arrived := make(chan struct{}, n)
	release := make(chan struct{})
	srv, downloads := tarballServer(t, name, version, componentTarball(t, name, version), func() {
		arrived <- struct{}{}
		<-release
	})

	dirs := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			dirs[i], errs[i] = cachingSource(srv, cache).Fetch(context.Background(), name, version)
		}(i)
	}
	timeout := time.After(20 * time.Second)
	for i := 0; i < n; i++ {
		select {
		case <-arrived:
		case <-timeout:
			close(release)
			wg.Wait()
			t.Fatalf("only %d of %d downloads reached the server; fetch errors %v", i, n, errs)
		}
	}
	close(release)
	wg.Wait()

	want := filepath.Join(cache, name, version)
	for i, err := range errs {
		if err != nil {
			t.Errorf("fetch %d: %v", i, err)
		} else if dirs[i] != want {
			t.Errorf("fetch %d returned %s, want %s", i, dirs[i], want)
		}
	}
	if c, err := LoadComponent(want); err != nil || c.Name != name || c.Version != version {
		t.Fatalf("cache entry: %+v, %v", c, err)
	}
	assertOnlyEntry(t, cache, name)
	assertOnlyEntry(t, filepath.Join(cache, name), version)

	// From now on the cache answers, without a download.
	if dir, err := cachingSource(srv, cache).Fetch(context.Background(), name, version); err != nil || dir != want {
		t.Fatalf("cached fetch: %q, %v", dir, err)
	}
	if got := downloads.Load(); got != n {
		t.Errorf("%d downloads, want %d (one per process, none once cached)", got, n)
	}
}

// The window of that race, held open in one Fetch: while it downloads,
// something appears at <cache>/<name>/<version>. A copy of the same version is
// what another process's rename leaves there, and Fetch uses it. Anything else
// is not this version: it stays as it is and the rename error is returned.
func TestFetchWhenTheCacheEntryAppearsDuringTheDownload(t *testing.T) {
	const name, version = "hertz-service", "0.70.13"
	for _, tc := range []struct {
		desc  string
		entry map[string]string // files that appear in the cache entry
		use   bool
	}{
		{"same version, from another process", map[string]string{
			ComponentFile:        `{"name":"hertz-service","version":"0.70.13"}`,
			"files/main.go.tmpl": "package main\n",
		}, true},
		{"another version", map[string]string{ComponentFile: `{"name":"hertz-service","version":"0.70.12"}`}, false},
		{"another component", map[string]string{ComponentFile: `{"name":"kitex-service","version":"0.70.13"}`}, false},
		{"broken component.json", map[string]string{ComponentFile: `{"name":`}, false},
		{"no component.json", map[string]string{"files/main.go.tmpl": "package main\n"}, false},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			cache := t.TempDir()
			dest := filepath.Join(cache, name, version)
			srv, _ := tarballServer(t, name, version, componentTarball(t, name, version), func() {
				for rel, content := range tc.entry {
					p := filepath.Join(dest, filepath.FromSlash(rel))
					if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
						t.Error(err)
					}
					if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
						t.Error(err)
					}
				}
			})

			dir, err := cachingSource(srv, cache).Fetch(context.Background(), name, version)
			if tc.use {
				if err != nil || dir != dest {
					t.Fatalf("Fetch = %q, %v; want the entry the other process left, %s", dir, err, dest)
				}
			} else {
				var le *os.LinkError
				if !errors.As(err, &le) || le.Op != "rename" {
					t.Fatalf("Fetch = %q, %v; want the rename error", dir, err)
				}
			}
			if got := filesUnder(t, dest); !maps.Equal(got, tc.entry) {
				t.Errorf("cache entry changed to %v", got)
			}
			assertOnlyEntry(t, cache, name)
		})
	}
}

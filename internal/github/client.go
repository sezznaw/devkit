// Package github is a minimal client for the parts of the GitHub API devkit
// needs: raw files, repository tarballs and release assets. A token is
// optional for public repositories (but avoids the 60 req/h anonymous limit)
// and required for private ones.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultHost = "github.com"

type Client struct {
	api   string // REST API root, e.g. https://api.github.com
	raw   string // raw content root, e.g. https://raw.githubusercontent.com
	token string
	http  *http.Client
}

// New builds a client for github.com or a GitHub Enterprise host.
func New(host, token string) *Client {
	host = strings.TrimPrefix(strings.TrimPrefix(strings.TrimRight(host, "/"), "https://"), "http://")
	if host == "" || host == DefaultHost {
		return NewWithBase("https://api.github.com", "https://raw.githubusercontent.com", token)
	}
	return NewWithBase("https://"+host+"/api/v3", "https://"+host+"/raw", token)
}

// NewWithBase builds a client with explicit API and raw-content roots (tests).
func NewWithBase(api, raw, token string) *Client {
	return &Client{
		api:   strings.TrimRight(api, "/"),
		raw:   strings.TrimRight(raw, "/"),
		token: token,
		http:  &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *Client) apiBase() string { return c.api }
func (c *Client) rawBase() string { return c.raw }

func (c *Client) do(ctx context.Context, u, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		msg := strings.TrimSpace(string(body))
		if resp.StatusCode == http.StatusForbidden && strings.Contains(msg, "rate limit") {
			msg = "API rate limit exceeded; set github_token to raise it"
		}
		return nil, fmt.Errorf("github: GET %s: %s: %s", u, resp.Status, msg)
	}
	return resp, nil
}

// RawFile returns the content of a file at ref, e.g. registry.json on main.
func (c *Client) RawFile(ctx context.Context, repo, path, ref string) ([]byte, error) {
	u := fmt.Sprintf("%s/%s/%s/%s", c.rawBase(), strings.Trim(repo, "/"), url.PathEscape(ref), path)
	resp, err := c.do(ctx, u, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// Tarball streams the repository archive at ref (whole repo; GitHub has no
// subpath option). The caller must close the returned reader.
func (c *Client) Tarball(ctx context.Context, repo, ref string) (io.ReadCloser, error) {
	u := fmt.Sprintf("%s/repos/%s/tarball/%s", c.apiBase(), strings.Trim(repo, "/"), url.PathEscape(ref))
	resp, err := c.do(ctx, u, "")
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// Release is the subset of the GitHub release object devkit needs.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

type Asset struct {
	Name string `json:"name"`
	// URL is the API asset URL; requesting it with Accept: application/octet-stream
	// downloads the file and works for private repositories too.
	URL  string `json:"url"`
	Size int64  `json:"size"`
}

func (c *Client) getJSON(ctx context.Context, u string, v any) error {
	resp, err := c.do(ctx, u, "application/vnd.github+json")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

// LatestRelease returns the newest non-prerelease, non-draft release.
func (c *Client) LatestRelease(ctx context.Context, repo string) (*Release, error) {
	var rel Release
	if err := c.getJSON(ctx, fmt.Sprintf("%s/repos/%s/releases/latest", c.apiBase(), strings.Trim(repo, "/")), &rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// ReleaseByTag returns the release for a tag, e.g. "v1.2.0".
func (c *Client) ReleaseByTag(ctx context.Context, repo, tag string) (*Release, error) {
	var rel Release
	if err := c.getJSON(ctx, fmt.Sprintf("%s/repos/%s/releases/tags/%s", c.apiBase(), strings.Trim(repo, "/"), url.PathEscape(tag)), &rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// DownloadAsset streams a release asset. The caller must close the reader.
func (c *Client) DownloadAsset(ctx context.Context, a Asset) (io.ReadCloser, error) {
	resp, err := c.do(ctx, a.URL, "application/octet-stream")
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// Asset finds a release asset by name.
func (r *Release) Asset(name string) (Asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

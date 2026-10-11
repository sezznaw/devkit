// Package gitlab is the small part of the GitLab REST API devkit needs:
// who the token belongs to, and the projects of a group (subgroups
// included) the token can see.
package gitlab

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	Host  string // https://gitlab.example.com
	Token string
	HTTP  *http.Client
}

func New(host, token string) *Client {
	return &Client{Host: strings.TrimRight(host, "/"), Token: token, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

type User struct {
	Username string `json:"username"`
	Name     string `json:"name"`
}

// Project is one repository.
type Project struct {
	ID                int    `json:"id"`
	Path              string `json:"path"`                // ser-member
	PathWithNamespace string `json:"path_with_namespace"` // sportsbook/backend/ser-member
	HTTPURL           string `json:"http_url_to_repo"`
	DefaultBranch     string `json:"default_branch"`
	Archived          bool   `json:"archived"`
	EmptyRepo         bool   `json:"empty_repo"`
}

func (c *Client) get(ctx context.Context, path string, q url.Values, v any) (http.Header, error) {
	u := c.Host + "/api/v4" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", c.Token)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if res.StatusCode == 401 {
		return nil, fmt.Errorf("%s: the token was refused (401): run devkit auth %s again", c.Host, c.Host)
	}
	if res.StatusCode/100 != 2 {
		return nil, fmt.Errorf("GET %s: %s: %s", u, res.Status, strings.TrimSpace(string(body)))
	}
	return res.Header, json.Unmarshal(body, v)
}

// User is the owner of the token.
func (c *Client) User(ctx context.Context) (*User, error) {
	var u User
	if _, err := c.get(ctx, "/user", nil, &u); err != nil {
		return nil, err
	}
	return &u, nil
}

// GroupProjects lists every project under group (a path such as
// "sportsbook"), subgroups included, that the token can see.
func (c *Client) GroupProjects(ctx context.Context, group string) ([]Project, error) {
	var all []Project
	for page := 1; ; page++ {
		q := url.Values{"include_subgroups": {"true"}, "per_page": {"100"}, "page": {strconv.Itoa(page)}, "simple": {"false"}, "order_by": {"path"}, "sort": {"asc"}}
		var batch []Project
		h, err := c.get(ctx, "/groups/"+url.PathEscape(group)+"/projects", q, &batch)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if next := h.Get("X-Next-Page"); next == "" || len(batch) == 0 {
			break
		}
	}
	return all, nil
}

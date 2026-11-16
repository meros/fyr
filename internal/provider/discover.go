package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/meros/fyr/internal/cli"
)

// The limits of a models request (V0.120).
const (
	fetchTimeout  = 15 * time.Second
	fetchMaxBytes = 2 << 20
	fetchMaxPages = 20
)

type fetchLimits struct {
	Timeout  time.Duration // a page; 0 is fetchTimeout
	MaxBytes int64         // a page; 0 is fetchMaxBytes
}

var errRedirect = errors.New("redirect to another host refused")

// fetchModels asks an endpoint for its models: GET <base>/v1/models, from
// the host, with the key as Claude Code sends it to that endpoint
// (x-api-key and anthropic-version), and as a Bearer token when that
// answers 401 or 404. Pages follow has_more and last_id (after_id), up to
// fetchMaxPages. A redirect may not leave the base URL's host. No error
// carries the key or the answer's body.
func fetchModels(ctx context.Context, c *http.Client, base, key string, lim fetchLimits) ([]cli.Model, error) {
	if lim.Timeout <= 0 {
		lim.Timeout = fetchTimeout
	}
	if lim.MaxBytes <= 0 {
		lim.MaxBytes = fetchMaxBytes
	}
	u, err := url.Parse(strings.TrimRight(base, "/") + "/v1/models")
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("the base URL is not an http(s) URL")
	}
	where := u.Host + u.Path
	cc := *c
	cc.Timeout = 0 // the context bounds each page
	cc.CheckRedirect = func(r *http.Request, via []*http.Request) error {
		if r.URL.Host != u.Host || r.URL.Scheme != u.Scheme || len(via) >= 5 {
			return errRedirect
		}
		return nil
	}
	bearer := false
	var out []cli.Model
	after := ""
	for page := 0; page < fetchMaxPages; page++ {
		pu := *u
		if after != "" {
			q := pu.Query()
			q.Set("after_id", after)
			pu.RawQuery = q.Encode()
		}
		body, status, err := get(ctx, &cc, pu.String(), key, bearer, lim)
		if err == nil && page == 0 && !bearer && (status == 401 || status == 404) {
			bearer = true
			body, status, err = get(ctx, &cc, pu.String(), key, bearer, lim)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %s", where, redact(err, key))
		}
		if status != 200 {
			return nil, fmt.Errorf("%s answered %d", where, status)
		}
		var r struct {
			Data []struct {
				ID          string `json:"id"`
				DisplayName string `json:"display_name"`
				Name        string `json:"name"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, fmt.Errorf("%s answered something that is not a models list", where)
		}
		for _, d := range r.Data {
			if id := strings.TrimSpace(d.ID); id != "" {
				name := d.DisplayName
				if name == "" {
					name = d.Name
				}
				out = append(out, cli.Model{ID: id, Name: name})
			}
		}
		if !r.HasMore || len(r.Data) == 0 {
			break
		}
		after = r.LastID
		if after == "" {
			after = r.Data[len(r.Data)-1].ID
		}
	}
	return out, nil
}

func get(ctx context.Context, c *http.Client, u, key string, bearer bool, lim fetchLimits) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, lim.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, 0, err
	}
	if bearer {
		req.Header.Set("Authorization", "Bearer "+key)
	} else {
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	resp, err := c.Do(req)
	if err != nil {
		switch {
		case errors.Is(err, errRedirect):
			return nil, 0, errRedirect
		case ctx.Err() != nil:
			return nil, 0, fmt.Errorf("no answer in %s", lim.Timeout)
		}
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // the URL is said once, by the caller
		}
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil, resp.StatusCode, nil
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, lim.MaxBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, fmt.Errorf("no answer in %s", lim.Timeout)
		}
		return nil, 0, err
	}
	if int64(len(b)) > lim.MaxBytes {
		return nil, 0, fmt.Errorf("the answer is over %d MB", lim.MaxBytes>>20)
	}
	return b, 200, nil
}

// redact keeps the key out of an error text, whatever produced it.
func redact(err error, key string) string {
	s := err.Error()
	if key != "" {
		s = strings.ReplaceAll(s, key, "…")
	}
	return s
}

package provider

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

const maxBodyForError = 512

// httpClient talks to the AI Gateway admin API. It authenticates with the
// full-admin API key (GATEWAY_ADMIN_API_KEY on the gateway) sent as
// `Authorization: Bearer <key>` — the same surface a human admin's OIDC token
// reaches, so the provider can manage providers, models, API keys, budgets and
// tenant settings. The key is never logged; error bodies are length-capped so
// a misbehaving proxy can't echo the bearer into CI logs.
type httpClient struct {
	base      string
	adminKey  string
	userAgent string
	http      *http.Client
}

func newClient(endpoint, adminKey, version string) *httpClient {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
	}
	return &httpClient{
		base:      endpoint,
		adminKey:  adminKey,
		userAgent: fmt.Sprintf("terraform-provider-aigateway/%s", version),
		http: &http.Client{
			Timeout:   30 * time.Second,
			Transport: tr,
		},
	}
}

type apiError struct {
	Status  int
	Code    string `json:"error_code"`
	Message string `json:"detail"`
}

func (e *apiError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("ai-gateway %d %s: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("ai-gateway %d: %s", e.Status, e.Message)
}

func isNotFound(err error) bool {
	var ae *apiError
	if errors.As(err, &ae) {
		return ae.Status == http.StatusNotFound
	}
	return false
}

// do issues a JSON request to the admin API. `body` is marshalled when non-nil;
// the response is decoded into `out` when non-nil. A 2xx with an empty body and
// a non-nil out is tolerated. >=400 returns an *apiError.
func (c *httpClient) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, reqBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.adminKey)
	req.Header.Set("X-Gateway-Admin-Key", c.adminKey)
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	tflog.Trace(ctx, "aigateway_request", map[string]any{"method": method, "path": path})

	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}

	tflog.Trace(ctx, "aigateway_response", map[string]any{"status": res.StatusCode, "bytes": len(raw)})

	if res.StatusCode >= 400 {
		ae := &apiError{Status: res.StatusCode}
		if json.Unmarshal(raw, ae) == nil && (ae.Code != "" || ae.Message != "") {
			// parsed structured error (gateway problem+json: detail/error_code)
		} else {
			snippet := string(raw)
			if len(snippet) > maxBodyForError {
				snippet = snippet[:maxBodyForError] + "…"
			}
			ae.Message = snippet
		}
		return ae
	}

	if out == nil {
		return nil
	}
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

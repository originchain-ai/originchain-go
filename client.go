package originchain

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config configures a [Client]. Only BaseURL and Bearer are required.
//
// BaseURL points at a per-tenant engine endpoint, e.g.
// "https://01HX...ap-south-1.db.originchain.ai". Trailing slashes are
// trimmed.
//
// Tenant is the tenant ID - the leftmost DNS label of the BaseURL host
// when omitted. Pass it explicitly only when the URL hostname doesn't
// match the tenant ID (e.g. behind a custom gateway).
//
// HTTP overrides the underlying [http.Client]. When nil the client uses
// its own [http.Client] with a 30-second timeout.
type Config struct {
	BaseURL string
	Bearer  string
	Tenant  string
	HTTP    *http.Client
}

// Client is the entry point for all OriginChain API calls. It is safe for
// concurrent use by multiple goroutines.
type Client struct {
	baseURL string
	bearer  string
	tenant  string
	http    *http.Client
	graph   *Graph
}

// NewClient builds a [Client] from cfg. Panics if BaseURL is empty.
//
// The returned client is goroutine-safe and reuses connections via the
// underlying [http.Client]'s transport. There is no Close method - the
// client holds no exclusive resources.
func NewClient(cfg Config) *Client {
	if cfg.BaseURL == "" {
		panic("originchain: Config.BaseURL is required")
	}
	httpc := cfg.HTTP
	if httpc == nil {
		httpc = &http.Client{Timeout: 30 * time.Second}
	}
	tenant := cfg.Tenant
	if tenant == "" {
		tenant = tenantFromBaseURL(cfg.BaseURL)
	}
	c := &Client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		bearer:  cfg.Bearer,
		tenant:  tenant,
		http:    httpc,
	}
	c.graph = &Graph{c: c}
	return c
}

// BaseURL returns the engine endpoint this client targets.
func (c *Client) BaseURL() string { return c.baseURL }

// Tenant returns the tenant ID this client targets.
func (c *Client) Tenant() string { return c.tenant }

// isMutatingMethod returns true for HTTP methods that change server state.
// Used to decide whether to auto-attach an Idempotency-Key header.
func isMutatingMethod(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// newIdempotencyKey returns a fresh UUIDv4 string (canonical hyphenated
// form). Uses stdlib crypto/rand so the SDK has zero third-party deps -
// `go.sum` stays empty.
func newIdempotencyKey() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand.Read never fails on supported platforms; if it
		// did the only sensible thing is to send an empty header and
		// let the caller's retry policy take over.
		return ""
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 1
	out := make([]byte, 36)
	hex.Encode(out[0:8], b[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], b[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], b[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], b[8:10])
	out[23] = '-'
	hex.Encode(out[24:36], b[10:16])
	return string(out)
}

// tenantFromBaseURL derives the tenant ID from the leftmost DNS label of
// baseURL's hostname. Returns empty string if baseURL doesn't parse - the
// caller must then set Config.Tenant explicitly.
func tenantFromBaseURL(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return ""
	}
	host := u.Hostname()
	if i := strings.IndexByte(host, '.'); i > 0 {
		return host[:i]
	}
	return host
}

// request is the shared HTTP plumbing every typed method runs on top of.
// It JSON-encodes body when non-nil, sets the bearer/User-Agent/Accept
// headers, dispatches the request with ctx, and either decodes the 2xx
// JSON body into out (when non-nil) or maps non-2xx into a typed error.
func (c *Client) request(
	ctx context.Context,
	method, path string,
	query url.Values,
	body any,
	out any,
) error {
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}

	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("originchain: encode request body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return fmt.Errorf("originchain: build request: %w", err)
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	req.Header.Set("User-Agent", "originchain-go/"+Version)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	// Auto-add Idempotency-Key on mutating methods so a network retry is
	// safe by default. The engine's idempotency cache is LRU-bounded
	// (10k entries + 24h TTL) so a fresh UUID per call is cheap; callers
	// that need cross-process retry semantics can set the header
	// explicitly via the per-request options.
	if isMutatingMethod(method) && req.Header.Get("Idempotency-Key") == "" {
		req.Header.Set("Idempotency-Key", newIdempotencyKey())
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("originchain: request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("originchain: read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return mapError(resp.StatusCode, respBody)
	}

	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("originchain: decode response: %w", err)
	}
	return nil
}

// mapError converts a non-2xx HTTP response into the right typed error.
// 402 with the canonical addon-required envelope produces an
// [*AddonRequiredError]; everything else surfaces as [*APIError] with
// the engine's machine-readable code when present.
func mapError(status int, body []byte) error {
	apiErr := &APIError{
		Status:  status,
		Code:    "http_error",
		Message: fmt.Sprintf("HTTP %d", status),
	}

	if len(body) == 0 {
		return apiErr
	}

	// Try to parse as JSON. Fall back to raw text on parse error.
	var generic map[string]any
	if err := json.Unmarshal(body, &generic); err != nil {
		apiErr.Message = string(body)
		return apiErr
	}

	// Pull the canonical control-plane shape: { "error": { "code", "message" } }.
	if errObj, ok := generic["error"].(map[string]any); ok {
		if code, ok := errObj["code"].(string); ok {
			apiErr.Code = code
		}
		if msg, ok := errObj["message"].(string); ok {
			apiErr.Message = msg
		}
	} else if errStr, ok := generic["error"].(string); ok {
		// Engine sometimes returns { "error": "<message>" } as a flat string.
		apiErr.Code = errStr
		apiErr.Message = errStr
	}

	// Engine 402 add-on envelope: { "error": "addon_required", "addon", ... }.
	if status == 402 {
		if errStr, _ := generic["error"].(string); errStr == "addon_required" {
			ae := &AddonRequiredError{APIError: *apiErr}
			ae.Code = "addon_required"
			if v, ok := generic["addon"].(string); ok {
				ae.Addon = v
			}
			if v, ok := generic["name"].(string); ok {
				ae.Name = v
			}
			if v, ok := generic["monthly_usd"].(float64); ok {
				ae.MonthlyUSD = v
			}
			if v, ok := generic["preview"].(bool); ok {
				ae.Preview = v
			}
			if v, ok := generic["purchase_url"].(string); ok {
				ae.PurchaseURL = v
			}
			if v, ok := generic["msg"].(string); ok {
				ae.Message = v
			}
			return ae
		}
	}

	// Engine sometimes returns { "msg": "..." } alongside the error code.
	if apiErr.Message == fmt.Sprintf("HTTP %d", status) {
		if v, ok := generic["msg"].(string); ok && v != "" {
			apiErr.Message = v
		}
	}

	return apiErr
}

// AsAPIError unwraps err into an *APIError, returning nil if err doesn't
// carry one. This is sugar for the common errors.As pattern when callers
// only need to read the status code.
func AsAPIError(err error) *APIError {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return nil
}

// AsAddonRequiredError unwraps err into an *AddonRequiredError, returning
// nil if err is some other shape.
func AsAddonRequiredError(err error) *AddonRequiredError {
	var addonErr *AddonRequiredError
	if errors.As(err, &addonErr) {
		return addonErr
	}
	return nil
}

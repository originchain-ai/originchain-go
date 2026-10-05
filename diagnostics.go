package originchain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Request correlation and opt-in client diagnostics (off by default).
//
// Every call carries X-OC-Logical-Request-Id (a fresh UUID) and X-OC-Attempt
// (always 1: this client does not retry). The engine records both next to its
// own request id (X-OC-Request-Id), which [APIError.RequestID] carries.
//
// With [Config.Diagnostics] set, the client also reports what it saw of each
// call - method, path, outcome, duration, the status it received or that nothing
// was sent or received, and the request ids - to the customer's own engine
// (POST /v1/tenants/:tenant/diagnostics, with the client's bearer). The engine
// matches the path to its route template and forwards only the template, so
// table, key and index names stay on that engine. Nothing else is sent: no SQL,
// no parameters, no row data, no search text, no error messages, no keys.
//
// Reporting never slows or fails a call: events go into a bounded queue (the
// oldest are dropped when it is full) and are sent in batches of up to 32, at
// most once a second, from a timer goroutine that exists only while a batch is
// waiting. A batch that cannot be sent is dropped.

const (
	headerLogicalRequestID = "X-OC-Logical-Request-Id"
	headerAttempt          = "X-OC-Attempt"
	diagMaxQueue           = 256
	diagMaxBatch           = 32
	diagFlushInterval      = time.Second
	diagSendTimeout        = 5 * time.Second
	// The engine refuses a whole diagnostics batch for one invalid event, so an
	// event that would break any of these rules is never queued.
	diagMaxPath       = 512
	diagMaxDurationMS = 3_600_000.0
)

var (
	engineIDRe  = regexp.MustCompile(`^[0-9a-f]{28}$`)
	sqlstateRe  = regexp.MustCompile(`^[0-9A-Z]{5}$`)
	lowerCodeRe = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+){0,3}$`)
	upperCodeRe = regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+){0,3}$`)
)

// diagnosticEvent is one call as the client saw it, in the engine's
// diagnostics contract.
type diagnosticEvent struct {
	Client           string  `json:"client"`
	ClientVersion    string  `json:"client_version"`
	Method           string  `json:"method"`
	Path             string  `json:"path"`
	Outcome          string  `json:"outcome"`
	DurationMS       float64 `json:"duration_ms"`
	HTTPStatus       int     `json:"http_status,omitempty"`
	ErrorCategory    string  `json:"error_category,omitempty"`
	ErrorCode        string  `json:"error_code,omitempty"`
	RequestID        string  `json:"request_id,omitempty"`
	LogicalRequestID string  `json:"logical_request_id"`
	Attempt          int     `json:"attempt"`
	Transport        string  `json:"transport,omitempty"`
}

// engineRequestID returns v when it is a request id the contract accepts.
func engineRequestID(v string) string {
	if engineIDRe.MatchString(v) || uuidRe.MatchString(v) {
		return v
	}
	return ""
}

// contractCode reports whether code is a SQLSTATE or a short snake-case or
// upper-case machine code (never a message).
func contractCode(code string) bool {
	if sqlstateRe.MatchString(code) {
		return true
	}
	digits := 0
	for _, r := range code {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	if len(code) > 32 || digits > 4 {
		return false
	}
	return lowerCodeRe.MatchString(code) || upperCodeRe.MatchString(code)
}

func classifyStatus(status int) (outcome, category string) {
	switch {
	case status < 400:
		return "success", ""
	case status == 400 || status == 422:
		return "error", "validation"
	case status == 401:
		return "denied", "auth"
	case status == 403:
		return "denied", "permission"
	case status == 404:
		return "error", "not_found"
	case status == 408:
		return "timeout", "timeout"
	case status == 409:
		return "error", "conflict"
	case status == 413:
		return "error", "capacity"
	case status == 429:
		return "error", "rate_limited"
	case status == 502 || status == 503 || status == 504:
		return "error", "unavailable"
	case status >= 500:
		return "error", "internal"
	}
	return "error", "unknown"
}

// classifyFailure maps a transport error (no response) to the contract.
func classifyFailure(err error) (outcome, category, transport string) {
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		// Never connected: the request was not sent.
		if opErr.Timeout() {
			return "timeout", "timeout", "not_sent"
		}
		return "error", "network", "not_sent"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled", "cancelled", "no_response"
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return "timeout", "timeout", "no_response"
	}
	return "error", "network", "no_response"
}

// newDiagnosticEvent builds the event for one call, or returns nil when it
// cannot be reported. Exactly one of status (> 0) and failure is set.
func newDiagnosticEvent(method, path string, started time.Time, logicalID string,
	status int, requestID, code string, failure error) *diagnosticEvent {
	method = strings.ToUpper(method)
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return nil
	}
	if !strings.HasPrefix(path, "/v1/tenants/") || len(path) > diagMaxPath ||
		!uuidRe.MatchString(logicalID) {
		return nil
	}
	ms := float64(time.Since(started).Microseconds()) / 1000
	e := &diagnosticEvent{
		Client:           "go",
		ClientVersion:    Version,
		Method:           method,
		Path:             path,
		DurationMS:       math.Min(math.Max(ms, 0), diagMaxDurationMS),
		LogicalRequestID: logicalID,
		Attempt:          1,
	}
	if failure != nil {
		e.Outcome, e.ErrorCategory, e.Transport = classifyFailure(failure)
		return e
	}
	e.Outcome, e.ErrorCategory = classifyStatus(status)
	e.HTTPStatus = status
	e.RequestID = requestID
	if e.ErrorCategory != "" && code != "http_error" && contractCode(code) {
		e.ErrorCode = code
	}
	return e
}

// diagnostics is the bounded queue and its sender.
type diagnostics struct {
	send func(ctx context.Context, batch []*diagnosticEvent)

	mu        sync.Mutex
	events    []*diagnosticEvent
	scheduled bool
	sending   sync.Mutex // one batch on the wire at a time
	dropped   int
}

func (d *diagnostics) push(e *diagnosticEvent) {
	if e == nil {
		return
	}
	d.mu.Lock()
	if len(d.events) >= diagMaxQueue {
		d.events = d.events[1:]
		d.dropped++
	}
	d.events = append(d.events, e)
	full := len(d.events) >= diagMaxBatch
	schedule := !d.scheduled
	d.scheduled = true
	d.mu.Unlock()
	switch {
	case full:
		go d.flush(context.Background())
	case schedule:
		time.AfterFunc(diagFlushInterval, func() { d.flush(context.Background()) })
	}
}

func (d *diagnostics) take() []*diagnosticEvent {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := len(d.events)
	if n > diagMaxBatch {
		n = diagMaxBatch
	}
	batch := append([]*diagnosticEvent(nil), d.events[:n]...)
	d.events = d.events[n:]
	if len(d.events) == 0 {
		d.scheduled = false
	}
	return batch
}

// flush sends everything queued, in batches. Failures are dropped.
func (d *diagnostics) flush(ctx context.Context) {
	d.sending.Lock()
	defer d.sending.Unlock()
	for {
		batch := d.take()
		if len(batch) == 0 {
			return
		}
		if ctx.Err() != nil {
			return
		}
		d.send(ctx, batch)
	}
}

// sendDiagnostics posts one batch straight to the engine, not through
// [Client.request]: a report never reports itself or carries correlation
// headers. Any failure is ignored.
func (c *Client) sendDiagnostics(ctx context.Context, batch []*diagnosticEvent) {
	body, err := json.Marshal(map[string]any{"events": batch})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, diagSendTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/v1/tenants/"+c.tenant+"/diagnostics", bytes.NewReader(body))
	if err != nil {
		return
	}
	if c.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearer)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "originchain-go/"+Version)
	resp, err := c.http.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// FlushDiagnostics sends any queued diagnostics now, for example before a
// short-lived process exits. It returns when they are sent, dropped, or ctx is
// done. A no-op when diagnostics are off.
func (c *Client) FlushDiagnostics(ctx context.Context) {
	if c.diag != nil {
		c.diag.flush(ctx)
	}
}

package originchain

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const testEngineID = "3f2a9c1b7e5d4a60000000000042"

// diagEngine answers engine calls with status and body, and records both the
// calls and every diagnostics batch posted to it.
type diagEngine struct {
	mu      sync.Mutex
	status  int
	body    string
	calls   []http.Header
	reports [][]map[string]any
}

func (e *diagEngine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if strings.HasSuffix(r.URL.Path, "/diagnostics") {
		var in struct {
			Events []map[string]any `json:"events"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		e.reports = append(e.reports, in.Events)
		w.WriteHeader(http.StatusAccepted)
		return
	}
	e.calls = append(e.calls, r.Header.Clone())
	w.Header().Set("X-OC-Request-Id", testEngineID)
	w.WriteHeader(e.status)
	_, _ = w.Write([]byte(e.body))
}

func (e *diagEngine) events() []map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []map[string]any
	for _, b := range e.reports {
		out = append(out, b...)
	}
	return out
}

func newDiagClient(t *testing.T, e *diagEngine, diagnostics bool) *Client {
	t.Helper()
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)
	return NewClient(Config{BaseURL: srv.URL, Bearer: "b", Tenant: "t", Diagnostics: diagnostics})
}

func TestCorrelation_FreshLogicalIDAndAttemptOnEveryCall(t *testing.T) {
	e := &diagEngine{status: 200, body: `{"kind":"select","rows":[]}`}
	c := newDiagClient(t, e, false)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := c.SQL(ctx, "SELECT 1 FROM t.x"); err != nil {
			t.Fatal(err)
		}
	}
	a, b := e.calls[0].Get(headerLogicalRequestID), e.calls[1].Get(headerLogicalRequestID)
	if !uuidRe.MatchString(a) || a == b {
		t.Fatalf("logical ids %q, %q: want two distinct UUIDs", a, b)
	}
	if got := e.calls[0].Get(headerAttempt); got != "1" {
		t.Fatalf("attempt = %q, want 1", got)
	}
}

func TestCorrelation_ErrorsCarryBothIDs(t *testing.T) {
	e := &diagEngine{status: 503, body: `{"error":{"code":"write_overloaded","message":"busy"}}`}
	c := newDiagClient(t, e, false)
	_, err := c.SQL(context.Background(), "SELECT 1 FROM t.x")
	apiErr := AsAPIError(err)
	if apiErr == nil {
		t.Fatalf("want *APIError, got %v", err)
	}
	if apiErr.RequestID != testEngineID {
		t.Errorf("RequestID = %q, want %q", apiErr.RequestID, testEngineID)
	}
	if apiErr.LogicalRequestID != e.calls[0].Get(headerLogicalRequestID) {
		t.Errorf("LogicalRequestID = %q, want the id that was sent", apiErr.LogicalRequestID)
	}
}

func TestCorrelation_AddonErrorCarriesIDs(t *testing.T) {
	e := &diagEngine{status: 402, body: `{"error":"addon_required","addon":"vector-search","name":"Vector Search"}`}
	c := newDiagClient(t, e, false)
	_, err := c.SQL(context.Background(), "SELECT 1 FROM t.x")
	addon := AsAddonRequiredError(err)
	if addon == nil || addon.RequestID != testEngineID || addon.LogicalRequestID == "" {
		t.Fatalf("addon error ids not set: %+v", addon)
	}
}

func TestDiagnostics_OffByDefault(t *testing.T) {
	e := &diagEngine{status: 200, body: `{"kind":"select","rows":[]}`}
	c := newDiagClient(t, e, false)
	_, _ = c.SQL(context.Background(), "SELECT 1 FROM t.x")
	c.FlushDiagnostics(context.Background())
	if n := len(e.events()); n != 0 {
		t.Fatalf("%d events reported with diagnostics off", n)
	}
}

func TestDiagnostics_ReportOnlyContractFields(t *testing.T) {
	e := &diagEngine{status: 429, body: `{"error":{"code":"rate_limited","message":"slow down"}}`}
	c := newDiagClient(t, e, true)
	_, _ = c.SQL(context.Background(), "SELECT secret FROM private_table")
	c.FlushDiagnostics(context.Background())
	events := e.events()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	allowed := map[string]bool{
		"client": true, "client_version": true, "method": true, "path": true, "outcome": true,
		"duration_ms": true, "http_status": true, "error_category": true, "error_code": true,
		"request_id": true, "logical_request_id": true, "attempt": true, "transport": true,
	}
	ev := events[0]
	raw, _ := json.Marshal(ev)
	for k := range ev {
		if !allowed[k] {
			t.Errorf("unexpected field %q", k)
		}
	}
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "private_table") {
		t.Errorf("query text leaked: %s", raw)
	}
	want := map[string]any{
		"client": "go", "client_version": Version, "method": "POST", "path": "/v1/tenants/t/sql",
		"outcome": "error", "error_category": "rate_limited", "error_code": "rate_limited",
		"http_status": float64(429), "request_id": testEngineID, "attempt": float64(1),
		"logical_request_id": e.calls[0].Get(headerLogicalRequestID),
	}
	for k, v := range want {
		if ev[k] != v {
			t.Errorf("%s = %v, want %v", k, ev[k], v)
		}
	}
}

func TestDiagnostics_SentInTheBackground(t *testing.T) {
	e := &diagEngine{status: 200, body: `{"kind":"select","rows":[]}`}
	c := newDiagClient(t, e, true)
	_, _ = c.SQL(context.Background(), "SELECT 1 FROM t.x")
	deadline := time.Now().Add(5 * time.Second)
	for len(e.events()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if len(e.events()) != 1 {
		t.Fatal("the timer did not send the queued event")
	}
}

func TestDiagnostics_NoResponseCarriesNoStatus(t *testing.T) {
	// A closed port: the dial fails, so the request was not sent.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	var sent []*diagnosticEvent
	c := NewClient(Config{BaseURL: "http://" + addr, Bearer: "b", Tenant: "t", Diagnostics: true})
	c.diag.send = func(_ context.Context, b []*diagnosticEvent) { sent = append(sent, b...) }
	if _, err := c.SQL(context.Background(), "SELECT 1 FROM t.x"); err == nil {
		t.Fatal("want a transport error")
	}
	c.FlushDiagnostics(context.Background())
	if len(sent) != 1 {
		t.Fatalf("got %d events, want 1", len(sent))
	}
	if ev := sent[0]; ev.Transport != "not_sent" || ev.HTTPStatus != 0 || ev.Outcome != "error" {
		t.Fatalf("event = %+v, want a not-sent failure without a status", ev)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if o, cat, tr := classifyFailure(ctx.Err()); o != "cancelled" || cat != "cancelled" || tr != "no_response" {
		t.Errorf("cancel = %s/%s/%s", o, cat, tr)
	}
	if o, _, tr := classifyFailure(context.DeadlineExceeded); o != "timeout" || tr != "no_response" {
		t.Errorf("deadline = %s/%s", o, tr)
	}
	if o, _, tr := classifyFailure(errors.New("connection reset")); o != "error" || tr != "no_response" {
		t.Errorf("reset = %s/%s", o, tr)
	}
}

func TestDiagnostics_NeverPoisonABatch(t *testing.T) {
	id := "8fb1dce6-72c0-4aa5-8d46-50a4e0b47ba5"
	now := time.Now()
	for name, ev := range map[string]*diagnosticEvent{
		"HEAD":       newDiagnosticEvent("HEAD", "/v1/tenants/t/sql", now, id, 200, "", "", nil),
		"non-tenant": newDiagnosticEvent("GET", "/v1/version", now, id, 200, "", "", nil),
		"long path":  newDiagnosticEvent("GET", "/v1/tenants/"+strings.Repeat("x", 600), now, id, 200, "", "", nil),
		"empty id":   newDiagnosticEvent("GET", "/v1/tenants/t/sql", now, "", 200, "", "", nil),
	} {
		if ev != nil {
			t.Errorf("%s: event queued, want nil", name)
		}
	}
	old := newDiagnosticEvent("GET", "/v1/tenants/t/sql?q=secret", now.Add(-2*time.Hour), id, 200, "", "", nil)
	if old == nil || old.DurationMS != diagMaxDurationMS || old.Path != "/v1/tenants/t/sql" {
		t.Fatalf("want a capped duration and no query string, got %+v", old)
	}
	for code, keep := range map[string]bool{
		"40001": true, "rate_limited": true, "WRITE_OVERLOADED": true,
		"http_error": false, "connection reset by peer": false, "E12345": false,
	} {
		ev := newDiagnosticEvent("GET", "/v1/tenants/t/sql", now, id, 500, "", code, nil)
		if (ev.ErrorCode != "") != keep {
			t.Errorf("code %q kept=%v, want %v", code, ev.ErrorCode != "", keep)
		}
	}
}

func TestDiagnostics_QueueIsBounded(t *testing.T) {
	d := &diagnostics{send: func(context.Context, []*diagnosticEvent) {}}
	d.scheduled = true // no timer, so nothing drains while the queue fills
	d.sending.Lock()   // and no full-batch flush either
	for i := 0; i < 300; i++ {
		d.push(&diagnosticEvent{Attempt: i})
	}
	d.mu.Lock()
	n, dropped, first := len(d.events), d.dropped, d.events[0].Attempt
	d.mu.Unlock()
	d.sending.Unlock()
	if n != diagMaxQueue || dropped != 44 || first != 44 {
		t.Fatalf("queue=%d dropped=%d first=%d, want 256, 44, 44", n, dropped, first)
	}
}

package originchain

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestServer wires an httptest server with handler and returns a Client
// pointed at it. Tenant is forced to "test-tenant" so URL assertions are
// stable regardless of the random hostname httptest picks.
func newTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient(Config{
		BaseURL: srv.URL,
		Bearer:  "test-bearer",
		Tenant:  "test-tenant",
	})
	return srv, c
}

// assertCommonHeaders verifies every Client request carries the auth and
// User-Agent headers the contract promises.
func assertCommonHeaders(t *testing.T, r *http.Request) {
	t.Helper()
	if got, want := r.Header.Get("Authorization"), "Bearer test-bearer"; got != want {
		t.Errorf("Authorization header = %q, want %q", got, want)
	}
	if got := r.Header.Get("User-Agent"); !strings.HasPrefix(got, "originchain-go/") {
		t.Errorf("User-Agent header = %q, want originchain-go/* prefix", got)
	}
	if got, want := r.Header.Get("Accept"), "application/json"; got != want {
		t.Errorf("Accept header = %q, want %q", got, want)
	}
}

// ── NewClient / Config ────────────────────────────────────────────────

func TestNewClient_DerivesTenantFromHostname(t *testing.T) {
	c := NewClient(Config{
		BaseURL: "https://01HX.ap-south-1.db.originchain.ai",
		Bearer:  "x",
	})
	if got, want := c.Tenant(), "01HX"; got != want {
		t.Errorf("Tenant = %q, want %q", got, want)
	}
}

func TestNewClient_ReEncodesFreeTierUUIDToULID(t *testing.T) {
	// Free-tier endpoints carry the raw instance UUID as the leftmost DNS
	// label, but engine /v1/tenants/:tenant/... paths want the ULID
	// re-encoding of those same 128 bits. Sending the UUID gets a 400
	// "invalid tenant id". Must match the TS SDK's uuidToUlid byte-for-byte.
	c := NewClient(Config{
		BaseURL: "https://0190b3e2-8f4a-7c1d-b5e6-0123456789ab.free.originchain.ai",
		Bearer:  "x",
	})
	if got, want := c.Tenant(), "01j2sy53tafgevbsg14d2pf2db"; got != want {
		t.Errorf("Tenant = %q, want %q (UUID must be re-encoded to ULID)", got, want)
	}
}

func TestUUIDToULID_MatchesTSVector(t *testing.T) {
	if got, want := uuidToULID("0190b3e2-8f4a-7c1d-b5e6-0123456789ab"), "01j2sy53tafgevbsg14d2pf2db"; got != want {
		t.Errorf("uuidToULID = %q, want %q", got, want)
	}
}

func TestNewClient_ExplicitTenantWins(t *testing.T) {
	c := NewClient(Config{
		BaseURL: "https://01HX.ap-south-1.db.originchain.ai",
		Bearer:  "x",
		Tenant:  "override",
	})
	if got, want := c.Tenant(), "override"; got != want {
		t.Errorf("Tenant = %q, want %q", got, want)
	}
}

func TestNewClient_PanicsOnEmptyBaseURL(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic for empty BaseURL")
		}
	}()
	NewClient(Config{Bearer: "x"})
}

// ── SQL ───────────────────────────────────────────────────────────────

func TestSQL_SelectDecodesRows(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonHeaders(t, r)
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/v1/tenants/test-tenant/sql" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got, want := r.Header.Get("Content-Type"), "application/json"; got != want {
			t.Errorf("Content-Type = %q, want %q", got, want)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["sql"] != "SELECT id FROM t" {
			t.Errorf("sql body = %v", body["sql"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"kind":"select","rows":[{"id":"1"},{"id":"2"}]}`)
	})

	resp, err := c.SQL(context.Background(), "SELECT id FROM t")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Kind != "select" {
		t.Errorf("Kind = %q", resp.Kind)
	}
	if len(resp.Rows) != 2 {
		t.Fatalf("Rows len = %d", len(resp.Rows))
	}
	if resp.Rows[0]["id"] != "1" {
		t.Errorf("first row id = %v", resp.Rows[0]["id"])
	}
}

// -- /sql wire contract ------------------------------------------------
//
// The engine's SqlResp is `#[serde(tag = "kind", rename_all = "lowercase")]`,
// so the literals below are byte-for-byte what the engine emits. They pin the
// shapes the old flat four-field struct silently discarded.

func TestSQL_InsertReturning(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"kind":"insert","schema":"shop.customers","inserted":2,`+
			`"returning":["id","email"],"rows":[{"id":"1","email":"a@b.c"},{"id":"2","email":"d@e.f"}]}`)
	})

	resp, err := c.SQL(context.Background(),
		"INSERT INTO shop.customers (email) VALUES ('a@b.c'),('d@e.f') RETURNING id, email")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Kind != KindInsert || resp.Schema != "shop.customers" {
		t.Fatalf("got %+v", resp)
	}
	// `inserted` is always present on an insert and used to be dropped.
	if resp.Inserted != 2 {
		t.Errorf("Inserted = %d, want 2", resp.Inserted)
	}
	if len(resp.Returning) != 2 || resp.Returning[0] != "id" {
		t.Errorf("Returning = %v", resp.Returning)
	}
	if len(resp.Rows) != 2 || resp.Rows[1]["email"] != "d@e.f" {
		t.Errorf("Rows = %v", resp.Rows)
	}
}

func TestSQL_PlainInsertOmitsReturningAndRows(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"kind":"insert","schema":"shop.customers","inserted":1}`)
	})

	resp, err := c.SQL(context.Background(), "INSERT INTO shop.customers (email) VALUES ('a@b.c')")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Inserted != 1 {
		t.Errorf("Inserted = %d, want 1", resp.Inserted)
	}
	// No RETURNING clause -> the engine omits both members. Rows must stay
	// nil so it is distinguishable from a RETURNING that matched no rows.
	if resp.Rows != nil {
		t.Errorf("Rows = %v, want nil", resp.Rows)
	}
	if resp.Returning != nil {
		t.Errorf("Returning = %v, want nil", resp.Returning)
	}
	if resp.Updated != nil || resp.Skipped != nil {
		t.Errorf("Updated/Skipped set on a plain insert: %v %v", resp.Updated, resp.Skipped)
	}
}

func TestSQL_Explain(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"kind":"explain","plan":"Limit(10)\n  Scan(shop.customers)"}`)
	})

	resp, err := c.SQL(context.Background(), "EXPLAIN SELECT * FROM shop.customers LIMIT 10")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Kind != KindExplain {
		t.Fatalf("Kind = %q, want %q", resp.Kind, KindExplain)
	}
	if !strings.Contains(resp.Plan, "Scan(shop.customers)") {
		t.Errorf("Plan = %q", resp.Plan)
	}
	// Plain EXPLAIN omits `stats`; only EXPLAIN ANALYZE sets it.
	if resp.Stats != nil {
		t.Errorf("Stats = %v, want nil", resp.Stats)
	}
}

func TestSQL_ExplainAnalyzeCarriesStats(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"kind":"explain","plan":"Scan(t)","stats":{"rows":3,"ms":1.5}}`)
	})

	resp, err := c.SQL(context.Background(), "EXPLAIN ANALYZE SELECT * FROM t")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Stats == nil || resp.Stats["rows"] != float64(3) {
		t.Errorf("Stats = %v", resp.Stats)
	}
}

func TestSQL_ScanDeleteOmitsPK(t *testing.T) {
	// A DELETE with an arbitrary predicate has no single row key, so the
	// engine omits `pk` entirely. A non-pointer PK reported "" here, which
	// is indistinguishable from a real empty-string key.
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"kind":"delete","schema":"shop.customers","rows_affected":7}`)
	})

	resp, err := c.SQL(context.Background(), "DELETE FROM shop.customers WHERE active = false")
	if err != nil {
		t.Fatal(err)
	}
	if resp.PK != nil {
		t.Errorf("PK = %q, want nil on a scan-predicate delete", *resp.PK)
	}
	if resp.RowsAffected == nil || *resp.RowsAffected != 7 {
		t.Errorf("RowsAffected = %v, want 7", resp.RowsAffected)
	}
	// Outside a transaction the buffered count is absent.
	if resp.RowsBuffered != nil {
		t.Errorf("RowsBuffered = %v, want nil", resp.RowsBuffered)
	}
}

func TestSQL_PKFastpathDeleteSetsPK(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"kind":"delete","schema":"shop.customers","pk":"c-1","rows_affected":1}`)
	})

	resp, err := c.SQL(context.Background(), "DELETE FROM shop.customers WHERE id = 'c-1'")
	if err != nil {
		t.Fatal(err)
	}
	if resp.PK == nil || *resp.PK != "c-1" {
		t.Errorf("PK = %v, want c-1", resp.PK)
	}
}

func TestSQL_Update(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"kind":"update","schema":"shop.customers","rows_affected":3}`)
	})

	resp, err := c.SQL(context.Background(), "UPDATE shop.customers SET active = true")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Kind != KindUpdate {
		t.Fatalf("Kind = %q", resp.Kind)
	}
	if resp.RowsAffected == nil || *resp.RowsAffected != 3 {
		t.Errorf("RowsAffected = %v, want 3", resp.RowsAffected)
	}
}

func TestSQL_TxControl(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"kind":"tx","op":"commit","ops_committed":4,"session_id":"sess-1"}`)
	})

	resp, err := c.SQL(context.Background(), "COMMIT")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Kind != KindTx || resp.Op != TxOpCommit {
		t.Fatalf("got %+v", resp)
	}
	if resp.OpsCommitted != 4 || resp.SessionID != "sess-1" {
		t.Errorf("got %+v", resp)
	}
}

func TestSQL_DDLKindsAreLowercasedWithoutSeparator(t *testing.T) {
	// serde's rename_all = "lowercase" does NOT insert an underscore:
	// CreateIndex serialises as "createindex", not "create_index".
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"kind":"createindex","schema":"shop.customers",`+
			`"index":"idx_email","rows_indexed":120,"created":true}`)
	})

	resp, err := c.SQL(context.Background(), "CREATE INDEX idx_email ON shop.customers (email)")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Kind != KindCreateIndex {
		t.Fatalf("Kind = %q, want %q", resp.Kind, KindCreateIndex)
	}
	if resp.Index != "idx_email" || resp.RowsIndexed != 120 {
		t.Errorf("got %+v", resp)
	}
	if resp.Created == nil || !*resp.Created {
		t.Errorf("Created = %v, want true", resp.Created)
	}
}

func TestSQL_SelectExposesProjectionColumns(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"kind":"select","rows":[{"email":"a@b.c","id":1}],"columns":["id","email"]}`)
	})

	resp, err := c.SQL(context.Background(), "SELECT id, email FROM shop.customers")
	if err != nil {
		t.Fatal(err)
	}
	// JSON object key order is NOT projection order - `columns` is.
	if len(resp.Columns) != 2 || resp.Columns[0] != "id" || resp.Columns[1] != "email" {
		t.Errorf("Columns = %v", resp.Columns)
	}
}

func TestSQL_ScalarProjectionWrapsRows(t *testing.T) {
	// A single-column projection emits bare scalars; they are normalised to
	// {"value": x} so Rows stays uniform.
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"kind":"select","rows":["a","b"]}`)
	})

	resp, err := c.SQL(context.Background(), "SELECT email FROM shop.customers")
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Rows) != 2 || resp.Rows[0]["value"] != "a" {
		t.Errorf("Rows = %v", resp.Rows)
	}
}

func TestSQL_SendsPositionalParams(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		params, ok := body["params"].([]any)
		if !ok || len(params) != 1 || params[0] != "c-1" {
			t.Errorf("params = %v", body["params"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"kind":"select","rows":[]}`)
	})

	if _, err := c.SQL(context.Background(),
		"SELECT * FROM shop.customers WHERE id = $1", "c-1"); err != nil {
		t.Fatal(err)
	}
}

func TestSQLOne_ReturnsFirstRow(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"kind":"select","rows":[{"id":"a"},{"id":"b"}]}`)
	})

	row, err := c.SQLOne(context.Background(), "SELECT id FROM t LIMIT 1")
	if err != nil {
		t.Fatal(err)
	}
	if row["id"] != "a" {
		t.Errorf("row = %v", row)
	}
}

func TestSQLOne_NilOnEmpty(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"kind":"select","rows":[]}`)
	})

	row, err := c.SQLOne(context.Background(), "SELECT id FROM t WHERE 1=0")
	if err != nil {
		t.Fatal(err)
	}
	if row != nil {
		t.Errorf("row = %v, want nil", row)
	}
}

func TestSQLOne_RejectsNonSelect(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"kind":"insert","schema":"s","inserted":1}`)
	})

	_, err := c.SQLOne(context.Background(), "INSERT INTO s VALUES (1)")
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr := AsAPIError(err)
	if apiErr == nil || apiErr.Code != "validation_failed" {
		t.Errorf("err = %v, want validation_failed APIError", err)
	}
}

// ── Vector ────────────────────────────────────────────────────────────

func TestVectorPut(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonHeaders(t, r)
		if r.Method != http.MethodPost {
			t.Errorf("method = %q", r.Method)
		}
		if r.URL.Path != "/v1/tenants/test-tenant/vector/embeds/put" {
			t.Errorf("path = %q", r.URL.Path)
		}
		// Mutating call must auto-attach Idempotency-Key so a retry of
		// the same logical call is safe under the engine's idempotency
		// cache. Without this assertion, a regression where the SDK
		// stops sending the header would silently turn every flaky
		// network into a duplicate write.
		if got := r.Header.Get("Idempotency-Key"); got == "" {
			t.Error("Idempotency-Key header missing on mutating call")
		}
		var body VectorPutRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.ID != "doc-1" || body.Dim != 3 || len(body.Embedding) != 3 {
			t.Errorf("body = %+v", body)
		}
		w.WriteHeader(204)
	})

	err := c.VectorPut(context.Background(), "embeds", VectorPutRequest{
		ID:        "doc-1",
		Embedding: []float32{0.1, 0.2, 0.3},
		Dim:       3,
		Metric:    "cosine",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestIdempotencyKey_GeneratedShape(t *testing.T) {
	// newIdempotencyKey() must produce canonical UUIDv4 strings - the
	// engine's idempotency cache treats the value as an opaque key, but
	// downstream tooling (CloudWatch filters, audit logs) expects the
	// dash-separated 36-char shape.
	k := newIdempotencyKey()
	if len(k) != 36 {
		t.Fatalf("len = %d, want 36 (got %q)", len(k), k)
	}
	if k[8] != '-' || k[13] != '-' || k[18] != '-' || k[23] != '-' {
		t.Errorf("missing UUID separators: %q", k)
	}
	if k[14] != '4' {
		t.Errorf("version nibble = %q, want '4'", k[14])
	}
	// Variant bits: high two bits of byte 8 must be 10 → first char is
	// in [8, 9, a, b].
	if !strings.ContainsRune("89ab", rune(k[19])) {
		t.Errorf("variant nibble = %q, want one of 8/9/a/b", k[19])
	}
	// Two consecutive calls must differ.
	if newIdempotencyKey() == k {
		t.Error("newIdempotencyKey returned the same value twice in a row")
	}
}

func TestIdempotencyKey_NotSentOnGET(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if got := r.Header.Get("Idempotency-Key"); got != "" {
			t.Errorf("GET should not carry Idempotency-Key, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[]`)
	})
	// FTS search is GET - the read path must NOT consume an idempotency
	// cache slot.
	if _, err := c.FTSSearch(context.Background(), "products", "description",
		FTSSearchRequest{Q: "cold", Mode: "bm25", K: 5}); err != nil {
		t.Fatal(err)
	}
}

func TestVectorTopK_HighRecall(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body VectorTopKRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Mode != ModeHighRecall {
			t.Errorf("mode = %q, want high_recall", body.Mode)
		}
		if body.K != 5 {
			t.Errorf("k = %d", body.K)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"a","score":0.92},{"id":"b","score":0.81}]`)
	})

	hits, err := c.VectorTopK(context.Background(), "embeds", VectorTopKRequest{
		Query: []float32{0.1, 0.2, 0.3},
		K:     5,
		Dim:   3,
		Mode:  ModeHighRecall,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("len = %d", len(hits))
	}
	if hits[0].ID != "a" || hits[0].Score != 0.92 {
		t.Errorf("hit[0] = %+v", hits[0])
	}
}

// ── FTS ───────────────────────────────────────────────────────────────

func TestFTSIndex(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/tenants/test-tenant/fts/docs/body" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.WriteHeader(204)
	})

	err := c.FTSIndex(context.Background(), "docs", "body", FTSIndexRequest{
		DocID: "1",
		Text:  "hello world",
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFTSSearch_BM25(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q", r.Method)
		}
		if got, want := r.URL.Query().Get("mode"), "bm25"; got != want {
			t.Errorf("mode = %q", got)
		}
		if got, want := r.URL.Query().Get("k"), "10"; got != want {
			t.Errorf("k = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"doc_id":"1","score":3.2},{"doc_id":"2","score":2.1}]`)
	})

	hits, err := c.FTSSearch(context.Background(), "docs", "body", FTSSearchRequest{
		Q:    "hello",
		Mode: "bm25",
		K:    10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].DocID != "1" || hits[0].Score != 3.2 {
		t.Errorf("hits = %+v", hits)
	}
}

func TestFTSSearch_Boolean(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `["1","2","3"]`)
	})

	hits, err := c.FTSSearch(context.Background(), "docs", "body", FTSSearchRequest{
		Q:    "hello",
		Mode: "boolean",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 3 || hits[0].DocID != "1" || hits[0].Score != 0.0 {
		t.Errorf("hits = %+v", hits)
	}
}

// ── Graph ─────────────────────────────────────────────────────────────

func TestGraphNeighbors(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/tenants/test-tenant/graph/social/neighbors" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("rel"); got != "follows" {
			t.Errorf("rel = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `["alice","bob"]`)
	})

	out, err := c.Graph().Neighbors(context.Background(), "social", NeighborsRequest{
		Rel: "follows",
		PK:  "carol",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].PK != "alice" || out[0].Depth != 1 {
		t.Errorf("out = %+v", out)
	}
}

func TestGraphReverseNeighbors(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/graph/social/reverse") {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `["dan"]`)
	})

	out, err := c.Graph().ReverseNeighbors(context.Background(), "social", NeighborsRequest{
		Rel: "follows",
		PK:  "carol",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].PK != "dan" {
		t.Errorf("out = %+v", out)
	}
}

func TestGraphBFS(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("max_depth"); got != "3" {
			t.Errorf("max_depth = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"pk":"a","depth":1},{"pk":"b","depth":2}]`)
	})

	hits, err := c.Graph().BFS(context.Background(), "social", BFSRequest{
		Rel:      "follows",
		PK:       "carol",
		MaxDepth: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[1].Depth != 2 {
		t.Errorf("hits = %+v", hits)
	}
}

func TestGraphPath(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"reachable":true}`)
	})

	res, err := c.Graph().Path(context.Background(), "social", PathRequest{
		Rel: "follows",
		Src: "a",
		Dst: "b",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Reachable {
		t.Error("reachable should be true")
	}
}

func TestGraphDijkstra_EncodesPerEdgeWeightsAsQueryParam(t *testing.T) {
	// The engine looks each traversed edge up by the literal key
	// "<from_pk>|<to_pk>" and SKIPS any edge the map does not cover, so a map
	// keyed by relation or column names silently reports every destination as
	// unreachable. Keep this keyed by edge - it is the contract.
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw := r.URL.Query().Get("weights_json")
		var weights map[string]float64
		if err := json.Unmarshal([]byte(raw), &weights); err != nil {
			t.Fatalf("weights_json = %q: %v", raw, err)
		}
		if weights["a|c"] != 1.0 || weights["c|b"] != 3.5 {
			t.Errorf("weights = %+v", weights)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"cost":4.5}`)
	})

	res, err := c.Graph().Dijkstra(context.Background(), "social", DijkstraRequest{
		Rel: "road",
		Src: "a",
		Dst: "b",
		Weights: map[string]float64{
			EdgeWeightKey("a", "c"): 1.0,
			EdgeWeightKey("c", "b"): 3.5,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Cost == nil || *res.Cost != 4.5 {
		t.Errorf("cost = %+v", res.Cost)
	}
}

func TestEdgeWeightKey(t *testing.T) {
	if got, want := EdgeWeightKey("n1", "n5"), "n1|n5"; got != want {
		t.Errorf("EdgeWeightKey = %q, want %q", got, want)
	}
}

func TestGraphDijkstra_NilCostUnreachable(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"cost":null}`)
	})

	res, err := c.Graph().Dijkstra(context.Background(), "social", DijkstraRequest{
		Rel: "road", Src: "a", Dst: "z",
		Weights: map[string]float64{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Cost != nil {
		t.Errorf("cost = %v, want nil", *res.Cost)
	}
}

// ── Ask ───────────────────────────────────────────────────────────────

func TestAsk(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["nl"] != "top customers" {
			t.Errorf("body = %v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"rows":[{"id":"1"}],"cache":"miss"}`)
	})

	resp, err := c.Ask(context.Background(), "top customers")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Cache != CacheMiss || len(resp.Rows) != 1 {
		t.Errorf("resp = %+v", resp)
	}
}

func TestAsk_SurfacesExplainAlongsidePlan(t *testing.T) {
	// `plan` and `explain` are gated on the same show_plan flag server-side,
	// so they appear together. `explain` used to be dropped on the floor.
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w,
			`{"rows":[],"cache":"hit","plan":{"op":"Scan"},"explain":{"op":"Scan","rows":0}}`)
	})

	resp, err := c.Ask(context.Background(), "anything")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Cache != CacheHit {
		t.Errorf("Cache = %q", resp.Cache)
	}
	if len(resp.Plan) == 0 {
		t.Error("Plan empty")
	}
	if len(resp.Explain) == 0 {
		t.Error("Explain empty - the engine sent it and the SDK dropped it")
	}
}

// -- Usage -------------------------------------------------------------

func TestUsage_SurfacesAddonCallCounters(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/tenants/test-tenant/usage" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"tenant":"test-tenant","used":{"store_keys":1},"schemas":[],`+
			`"addon_calls":[{"addon":"vector-search","allowed":12,"rejected":3}]}`)
	})

	u, err := c.Usage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(u.AddonCalls) != 1 || u.AddonCalls[0].Addon != "vector-search" {
		t.Fatalf("AddonCalls = %+v", u.AddonCalls)
	}
	if u.AddonCalls[0].Allowed != 12 || u.AddonCalls[0].Rejected != 3 {
		t.Errorf("AddonCalls[0] = %+v", u.AddonCalls[0])
	}
}

// ── Error mapping ─────────────────────────────────────────────────────

func TestError_401MapsToAPIError(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(401)
		_, _ = io.WriteString(w, `{"error":{"code":"unauthorized","message":"bad token"}}`)
	})

	_, err := c.SQL(context.Background(), "SELECT 1")
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("not APIError: %v", err)
	}
	if apiErr.Status != 401 || apiErr.Code != "unauthorized" {
		t.Errorf("apiErr = %+v", apiErr)
	}
	if !strings.Contains(apiErr.Error(), "bad token") {
		t.Errorf("Error() = %q", apiErr.Error())
	}
}

func TestError_402MapsToAddonRequiredError(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(402)
		_, _ = io.WriteString(w, `{
			"error": "addon_required",
			"addon": "vector-search",
			"name":  "Vector Search",
			"monthly_usd": 49,
			"preview": false,
			"purchase_url": "https://app.originchain.ai/billing/addons?enable=vector-search",
			"msg": "Enable Vector Search to use this endpoint."
		}`)
	})

	_, err := c.VectorTopK(context.Background(), "embeds", VectorTopKRequest{
		Query: []float32{0.1}, K: 1, Dim: 1,
	})
	if err == nil {
		t.Fatal("expected error")
	}

	// Both errors.As branches should match - AddonRequiredError embeds APIError.
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("not APIError: %v", err)
	}
	if apiErr.Status != 402 {
		t.Errorf("status = %d", apiErr.Status)
	}

	var addonErr *AddonRequiredError
	if !errors.As(err, &addonErr) {
		t.Fatalf("not AddonRequiredError: %v", err)
	}
	if addonErr.Addon != "vector-search" || addonErr.Name != "Vector Search" {
		t.Errorf("addonErr = %+v", addonErr)
	}
	if addonErr.MonthlyUSD != 49 {
		t.Errorf("monthly_usd = %v", addonErr.MonthlyUSD)
	}
	if addonErr.PurchaseURL == "" {
		t.Error("PurchaseURL empty")
	}
}

func TestError_404MapsToAPIError(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_, _ = io.WriteString(w, `{"error":{"code":"not_found","message":"schema missing"}}`)
	})

	_, err := c.SQL(context.Background(), "SELECT 1")
	apiErr := AsAPIError(err)
	if apiErr == nil || apiErr.Status != 404 || apiErr.Code != "not_found" {
		t.Errorf("apiErr = %+v", apiErr)
	}
}

func TestError_5xxMapsToAPIError(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(503)
		_, _ = io.WriteString(w, `{"error":{"code":"unavailable","message":"engine restarting"}}`)
	})

	_, err := c.SQL(context.Background(), "SELECT 1")
	apiErr := AsAPIError(err)
	if apiErr == nil || apiErr.Status != 503 {
		t.Errorf("apiErr = %+v", apiErr)
	}
}

func TestError_NonJSONBodyFallsBack(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, "internal server error")
	})

	_, err := c.SQL(context.Background(), "SELECT 1")
	apiErr := AsAPIError(err)
	if apiErr == nil || apiErr.Status != 500 {
		t.Fatalf("apiErr = %+v", apiErr)
	}
	if !strings.Contains(apiErr.Message, "internal server error") {
		t.Errorf("message = %q", apiErr.Message)
	}
}

// errors.Is loose-match by status - useful for retry policies.
func TestAPIError_IsByStatus(t *testing.T) {
	a := &APIError{Status: 503, Code: "x", Message: "m"}
	b := &APIError{Status: 503}
	if !errors.Is(a, b) {
		t.Error("503 should match 503")
	}
	c := &APIError{Status: 401}
	if errors.Is(a, c) {
		t.Error("503 should not match 401")
	}
	any := &APIError{}
	if !errors.Is(a, any) {
		t.Error("any-status sentinel should match")
	}
}

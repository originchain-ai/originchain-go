package originchain

import "encoding/json"

// SQL response Kind values. The engine serialises its SqlResp enum with
// serde's `tag = "kind", rename_all = "lowercase"`, so the discriminator is
// the "kind" member and multi-word variants lowercase WITHOUT a separator -
// CreateTable is "createtable", not "create_table".
const (
	KindExplain         = "explain"
	KindSelect          = "select"
	KindInsert          = "insert"
	KindUpdate          = "update"
	KindDelete          = "delete"
	KindBuffered        = "buffered"
	KindTx              = "tx"
	KindCreateTable     = "createtable"
	KindDropTable       = "droptable"
	KindAlterTable      = "altertable"
	KindCreateIndex     = "createindex"
	KindCreateView      = "createview"
	KindDropView        = "dropview"
	KindCreateSequence  = "createsequence"
	KindDropSequence    = "dropsequence"
	KindCreateProcedure = "createprocedure"
	KindDropProcedure   = "dropprocedure"
	KindCreateFunction  = "createfunction"
	KindDropFunction    = "dropfunction"
)

// Transaction ops reported by [SQLResponse.Op] when Kind == [KindTx].
const (
	TxOpBegin    = "begin"
	TxOpCommit   = "commit"
	TxOpRollback = "rollback"
	TxOpNoop     = "noop"
)

// SQLResponse is the internally-tagged union response from
// POST /v1/tenants/:t/sql. Kind selects which fields are populated; branch on
// it against the Kind* constants before reading anything else.
//
//	resp, err := oc.SQL(ctx, "EXPLAIN SELECT * FROM shop.customers")
//	switch resp.Kind {
//	case originchain.KindSelect:  use(resp.Rows)
//	case originchain.KindExplain: use(resp.Plan)
//	case originchain.KindInsert:  use(resp.Inserted)
//	}
//
// The engine omits every optional member from the wire rather than sending
// null (serde `skip_serializing_if`), so a pointer field being nil means the
// engine did not report that quantity for this statement - it is NOT a zero.
// That distinction is load-bearing on DELETE, where PK is nil for a
// scan-predicate delete and set only on the WHERE <pk> = <literal> fastpath,
// and on RowsAffected / RowsBuffered, which are mutually exclusive
// (outside vs inside a transaction).
//
// Mirrors the engine's preview_endpoints.rs::SqlResp. Members are grouped by
// the Kind that populates them.
type SQLResponse struct {
	// Kind is the wire discriminator - one of the Kind* constants.
	Kind string `json:"kind"`

	// ── Kind == KindSelect ──
	//
	// Rows carries the projected rows. A single-column projection emits bare
	// scalars on the wire; those are normalised to map[string]any{"value": x}
	// so Rows is uniform. Rows is also populated on insert/delete RETURNING.
	Rows []map[string]any `json:"rows,omitempty"`
	// Columns lists the output columns in projection (SELECT-list) order when
	// the plan declares one. Empty for SELECT *, join-*, and set-op plans -
	// the engine omits it. Decode positionally off this rather than trusting
	// JSON object key order.
	Columns []string `json:"columns,omitempty"`

	// ── Kind == KindExplain ──
	//
	// Plan is the pretty-printed plan tree. Stats carries per-operator runtime
	// stats and is populated only by EXPLAIN ANALYZE.
	Plan  string         `json:"plan,omitempty"`
	Stats map[string]any `json:"stats,omitempty"`

	// ── Kinds that name a table ──
	//
	// Schema is the "<namespace>.<table>" id. Set on insert, update, delete,
	// buffered, createtable, droptable, altertable, and createindex.
	Schema string `json:"schema,omitempty"`

	// ── Kind == KindInsert ──
	//
	// Inserted counts NEWLY-inserted rows and is always present on an insert.
	// Inside an open transaction it counts rows buffered for COMMIT.
	Inserted int `json:"inserted,omitempty"`
	// Updated is set only by ON CONFLICT DO UPDATE: existing rows rewritten
	// by the conflict merge. Nil on every other insert shape.
	Updated *uint64 `json:"updated,omitempty"`
	// Skipped is set only by ON CONFLICT DO NOTHING: conflicting proposed
	// rows that were dropped. Nil on every other insert shape.
	Skipped *uint64 `json:"skipped,omitempty"`

	// ── Kind == KindInsert / KindDelete / KindUpdate ──
	//
	// Returning echoes the RETURNING column list; Rows above then carries the
	// written or deleted rows projected by it. Both are absent together when
	// the statement had no RETURNING clause. UPDATE ... RETURNING is refused
	// by the translator today, so Returning is always nil on an update.
	Returning []string `json:"returning,omitempty"`

	// ── Kind == KindDelete ──
	//
	// PK is the row key on the WHERE <pk> = <literal> fastpath. Nil for a
	// scan-predicate delete, which has no single row key.
	PK *string `json:"pk,omitempty"`
	// RowsBuffered counts row-removal ops buffered inside an open
	// transaction. Nil outside a transaction.
	RowsBuffered *uint64 `json:"rows_buffered,omitempty"`

	// ── Kind == KindDelete / KindUpdate ──
	//
	// RowsAffected counts rows the writer actually removed or rewrote outside
	// a transaction. On a delete it is nil inside a transaction (see
	// RowsBuffered); on an update it is always present.
	RowsAffected *uint64 `json:"rows_affected,omitempty"`

	// ── Kind == KindBuffered ──
	//
	// Shard is the peer node that owns the table, at buffering time. The
	// statement was buffered verbatim and the owning node validates it at
	// COMMIT, so constraint errors surface as a 409 on COMMIT, not here.
	Shard *uint32 `json:"shard,omitempty"`

	// ── Kind == KindTx ──
	//
	// Op is one of the TxOp* constants. OpsCommitted is populated on commit so
	// the caller can confirm the buffer wasn't empty.
	Op           string `json:"op,omitempty"`
	OpsCommitted int    `json:"ops_committed,omitempty"`
	SessionID    string `json:"session_id,omitempty"`

	// ── DDL kinds ──
	//
	// Index / RowsIndexed are set by createindex; RowsIndexed counts the
	// existing rows backfilled into the new index.
	Index       string `json:"index,omitempty"`
	RowsIndexed uint64 `json:"rows_indexed,omitempty"`
	// View is set by createview / dropview; Sequence by createsequence /
	// dropsequence; Name by create/drop procedure and create/drop function.
	View     string `json:"view,omitempty"`
	Sequence string `json:"sequence,omitempty"`
	Name     string `json:"name,omitempty"`
	// Created is false only for IF NOT EXISTS on an object that already
	// existed; Dropped is false only for IF EXISTS on one that wasn't
	// registered. Nil on kinds that report neither.
	Created *bool `json:"created,omitempty"`
	Dropped *bool `json:"dropped,omitempty"`
	// Migration / State / Ops are set by altertable. ALTER runs synchronously,
	// so when the response returns the schema change is live. State is
	// "Completed", or "noop" (with an empty Migration) when nothing changed.
	Migration string `json:"migration,omitempty"`
	State     string `json:"state,omitempty"`
	Ops       int    `json:"ops,omitempty"`
}

// sqlResponseFields is a defined type over SQLResponse. A defined type does
// NOT inherit the original's methods, so embedding it below keeps
// SQLResponse.UnmarshalJSON off sqlResponseWire's method set - without that,
// decoding into the wire struct would re-enter UnmarshalJSON forever.
type sqlResponseFields SQLResponse

// sqlResponseWire mirrors SQLResponse but takes Rows as raw JSON, because a
// single-column projection emits bare scalars where SQLResponse.Rows wants
// objects. The outer Rows shadows the embedded one (shallower field wins in
// encoding/json), so "rows" decodes as raw.
type sqlResponseWire struct {
	sqlResponseFields
	Rows []json.RawMessage `json:"rows,omitempty"`
}

// UnmarshalJSON decodes the engine's /sql envelope, normalising scalar row
// entries into map[string]any{"value": x} so Rows is uniform regardless of
// projection width. Rows stays nil when the engine omitted the member, which
// is how a plain INSERT is told apart from an INSERT ... RETURNING that
// matched no rows.
func (r *SQLResponse) UnmarshalJSON(b []byte) error {
	var wire sqlResponseWire
	if err := json.Unmarshal(b, &wire); err != nil {
		return err
	}
	*r = SQLResponse(wire.sqlResponseFields)
	if wire.Rows == nil {
		r.Rows = nil
		return nil
	}
	r.Rows = make([]map[string]any, 0, len(wire.Rows))
	for _, raw := range wire.Rows {
		var obj map[string]any
		if err := json.Unmarshal(raw, &obj); err == nil && obj != nil {
			r.Rows = append(r.Rows, obj)
			continue
		}
		// Scalar projection - wrap so Rows stays uniform.
		var scalar any
		if err := json.Unmarshal(raw, &scalar); err == nil {
			r.Rows = append(r.Rows, map[string]any{"value": scalar})
		}
	}
	return nil
}

// VectorPutRequest is the body for POST /v1/tenants/:t/vector/:table/put.
//
// Dim is required because the substrate validates the embedding length
// against the table's configured dimensionality on every put.
//
// Metric is one of the Metric* constants and is checked against the first
// put's choice for the table - changing it after the index is built returns
// 400. An unrecognised value is rejected with a 400 rather than silently
// falling back to cosine. Leaving it empty omits the member and the engine
// defaults to cosine.
type VectorPutRequest struct {
	ID        string         `json:"id"`
	Embedding []float32      `json:"embedding"`
	Dim       int            `json:"dim"`
	Metric    string         `json:"metric,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// VectorTopKRequest is the body for POST /v1/tenants/:t/vector/:table/topk.
//
// Mode selects the recall/latency profile: [ModeFast] favours latency,
// [ModeHighRecall] favours recall. When omitted the server defaults to
// high_recall. Metric is one of the Metric* constants. Both are validated
// server-side - an unrecognised value is a 400, not a silent default.
//
// Filter is a metadata equality filter - non-empty filters force an HNSW +
// post-filter codepath server-side.
type VectorTopKRequest struct {
	Query  []float32      `json:"query"`
	K      int            `json:"k"`
	Dim    int            `json:"dim"`
	Metric string         `json:"metric,omitempty"`
	Filter map[string]any `json:"filter,omitempty"`
	Mode   VectorMode     `json:"mode,omitempty"`
}

// VectorHit is one topk result. Score semantics depend on the metric the
// index was built with - cosine and dot return higher-is-closer, l2
// returns lower-is-closer. The SDK does not re-sort or normalise; the
// substrate already returns hits in the right order for the metric.
type VectorHit struct {
	ID    string  `json:"id"`
	Score float64 `json:"score"`
}

// FTSIndexRequest is the body for POST /v1/tenants/:t/fts/:table/:field.
// Per-tenant per-field inverted index; subsequent calls with the same
// DocID overwrite.
type FTSIndexRequest struct {
	DocID string `json:"doc_id"`
	Text  string `json:"text"`
}

// FTSSearchRequest is the query parameters for GET /v1/tenants/:t/fts/:table/:field.
//
// Mode selects the matching strategy:
//
//   - "boolean" (default) AND-matches all tokens and returns unranked
//     doc_ids; Score is set to 0.0 in [FTSHit].
//   - "bm25" returns the top-K hits ranked by BM25.
//   - "phrase" requires the tokens in order; Score is 0.0.
//
// All three modes return [FTSHit] for a uniform shape.
type FTSSearchRequest struct {
	Q    string
	Mode string
	K    int
}

// FTSHit is one full-text result. Boolean / phrase modes set Score to 0.0;
// bm25 mode populates it with the BM25 ranking weight.
type FTSHit struct {
	DocID string  `json:"doc_id"`
	Score float64 `json:"score"`
}

// NeighborsRequest is the query parameters for /graph/:schema/neighbors
// and /graph/:schema/reverse. Rel names the relation in the schema TOML;
// PK is the source primary key.
type NeighborsRequest struct {
	Rel string
	PK  string
}

// Neighbor is one neighbour PK from the one-hop graph endpoints. Depth is
// always 1 for these endpoints; multi-hop traversals use [BFSHit] instead.
type Neighbor struct {
	PK    string `json:"pk"`
	Depth int    `json:"depth"`
}

// BFSRequest is the query parameters for /graph/:schema/bfs. MaxDepth caps
// the traversal depth; leaving it zero omits the parameter and the engine
// applies its own default of 3 hops. It is a default, not an unlimited
// traversal with a safety clamp - set MaxDepth explicitly to go deeper.
type BFSRequest struct {
	Rel      string
	PK       string
	MaxDepth int
}

// BFSHit is one BFS-traversal result. Depth is the BFS distance from the
// source PK.
type BFSHit struct {
	PK    string `json:"pk"`
	Depth int    `json:"depth"`
}

// PathRequest is the query parameters for /graph/:schema/path. Reachability
// is checked from Src to Dst along Rel up to MaxDepth hops; leaving MaxDepth
// zero omits the parameter and the engine applies its own default of 3 hops.
type PathRequest struct {
	Rel      string
	Src      string
	Dst      string
	MaxDepth int
}

// PathResult is the response from /graph/:schema/path. Reachable is the
// only field the substrate returns in v0 - the actual path itself is not
// materialised. v1 will surface the edge list.
type PathResult struct {
	Reachable bool `json:"reachable"`
}

// DijkstraRequest is the query parameters for /graph/:schema/dijkstra.
//
// Weights is a PER-EDGE map keyed "<from_pk>|<to_pk>" - build keys with
// [EdgeWeightKey]. It is NOT a map of relation or column names. The engine
// looks every traversed edge up by that exact key and SKIPS any edge the map
// does not cover, so a wrongly-keyed map silently reports every destination
// as unreachable (Cost nil) instead of returning an error.
//
// The SDK JSON-encodes the map into the weights_json query parameter the
// engine reads. Negative or NaN weights are rejected with a 400.
type DijkstraRequest struct {
	Rel     string
	Src     string
	Dst     string
	Weights map[string]float64
}

// EdgeWeightKey builds the [DijkstraRequest.Weights] key for the edge
// from -> to. The engine keys its weight lookup on "<from_pk>|<to_pk>" and
// skips edges it cannot find, so getting this wrong reports every
// destination as unreachable rather than erroring.
func EdgeWeightKey(from, to string) string { return from + "|" + to }

// DijkstraResult is the response from /graph/:schema/dijkstra. Cost is nil
// when Dst is unreachable from Src under the supplied weight function,
// otherwise it points to the total weight along the cheapest path.
type DijkstraResult struct {
	Cost *float64 `json:"cost"`
}

// AskResponse is the response from POST /v1/tenants/:t/ask.
//
// Rows is the engine-evaluated result of the natural-language plan. Cache is
// the planner-cache disposition and is exactly one of [CacheHit] or
// [CacheMiss] - the engine emits no third value.
//
// Plan and Explain are gated on the SAME show_plan flag server-side: both are
// omitted from the wire when it is false, so they appear and disappear
// together. Plan is the executed plan; Explain is its rendered explain tree.
type AskResponse struct {
	Rows    []map[string]any `json:"rows"`
	Cache   string           `json:"cache"`
	Plan    json.RawMessage  `json:"plan,omitempty"`
	Explain json.RawMessage  `json:"explain,omitempty"`
}

// Planner-cache dispositions reported by [AskResponse.Cache].
const (
	CacheHit  = "hit"
	CacheMiss = "miss"
)

// TenantConfiguration is the neutral, spec-based compute configuration
// returned by GET /v1/tenants/:t/usage.
//
// It REPLACES the internal weather codename (thunder/storm/cyclone/…)
// the engine used to surface - the SDK never exposes that codename.
// Slug is the stable machine id (entry/standard/advanced/custom); Label
// is display text such as "4 vCPU / 16 GB, HA". VCPU, RAMGB, StorageGB,
// and MonthlyPrice are nil for the sales-sized "custom" configuration.
type TenantConfiguration struct {
	Slug         string `json:"slug"`
	Label        string `json:"label"`
	HA           bool   `json:"ha"`
	VCPU         *int   `json:"vcpu,omitempty"`
	RAMGB        *int   `json:"ram_gb,omitempty"`
	StorageGB    *int64 `json:"storage_gb,omitempty"`
	MonthlyPrice *int   `json:"monthly_price,omitempty"`
}

// AddonCallUsage is one per-add-on entitlement-gate line from /usage: how
// many calls the gate Allowed versus Rejected (402'd) for that add-on.
type AddonCallUsage struct {
	Addon    string `json:"addon"`
	Allowed  uint64 `json:"allowed"`
	Rejected uint64 `json:"rejected"`
}

// SchemaUsage is one per-schema row/byte/segment line from /usage.
type SchemaUsage struct {
	Schema   string `json:"schema"`
	Rows     int64  `json:"rows"`
	Bytes    int64  `json:"bytes"`
	Segments int64  `json:"segments"`
}

// TenantUsage is the response of GET /v1/tenants/:t/usage.
//
// Tier is the neutral configuration slug (entry/standard/advanced/
// custom) - never the internal weather codename, which the SDK does not
// expose. Prefer Configuration for the full spec. Tier, Configuration,
// and Limits are absent (zero / nil) in legacy per-addon mode. Limits
// is uint64 because the "custom" envelope uses the u64::MAX fair-use
// sentinel.
type TenantUsage struct {
	Tenant        string               `json:"tenant"`
	Tier          string               `json:"tier,omitempty"`
	Configuration *TenantConfiguration `json:"configuration,omitempty"`
	Limits        map[string]uint64    `json:"limits,omitempty"`
	Used          map[string]any       `json:"used"`
	Schemas       []SchemaUsage        `json:"schemas"`
	AddonCalls    []AddonCallUsage     `json:"addon_calls,omitempty"`
}

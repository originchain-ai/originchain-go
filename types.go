package originchain

import "encoding/json"

// SQLResponse is the discriminated-union response from POST /v1/tenants/:t/sql.
//
// The engine routes the parsed statement to one of three shapes:
//
//   - Kind == "select": the substrate ran the SELECT and Rows holds the
//     decoded JSON value list - each row is whatever shape the schema's
//     projection emits.
//   - Kind == "insert": the engine translated the INSERT into a typed
//     payload for /rows/:schema; Schema is set, Rows holds the rows the
//     caller is expected to re-issue with idempotency. Writes through
//     /sql aren't auto-executed in v0.
//   - Kind == "delete": the engine translated the DELETE into a typed PK
//     for /rows/:schema/:pk; Schema and PK are set.
//
// The split exists because writes from /sql need the idempotency-key
// plumbing the typed row endpoints already have. See the engine's
// preview_endpoints.rs::sql_exec for the full contract.
type SQLResponse struct {
	Kind   string           `json:"kind"`
	Rows   []map[string]any `json:"rows,omitempty"`
	Schema string           `json:"schema,omitempty"`
	PK     string           `json:"pk,omitempty"`
}

// VectorPutRequest is the body for POST /v1/tenants/:t/vector/:table/put.
// Dim is required because the substrate validates the embedding length
// against the table's configured dimensionality on every put. Metric is
// one of "cosine" / "dot" / "l2" and is checked against the first put's
// choice for the table - changing it after the index is built returns 400.
type VectorPutRequest struct {
	ID        string         `json:"id"`
	Embedding []float32      `json:"embedding"`
	Dim       int            `json:"dim"`
	Metric    string         `json:"metric,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// VectorTopKRequest is the body for POST /v1/tenants/:t/vector/:table/topk.
// Mode selects the recall/latency profile: [ModeFast] favours latency,
// [ModeHighRecall] favours recall. When omitted the server defaults to
// "high_recall". Filter is a metadata equality filter - non-empty filters
// force an HNSW + post-filter codepath server-side.
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

// BFSRequest is the query parameters for /graph/:schema/bfs. MaxDepth
// caps the traversal depth; the engine clamps to a sane upper bound.
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
// is checked from Src to Dst along Rel up to MaxDepth hops.
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
// Weights is a per-relation-attribute weight map; the SDK JSON-encodes it
// into the weights_json query parameter the engine reads.
type DijkstraRequest struct {
	Rel     string
	Src     string
	Dst     string
	Weights map[string]float64
}

// DijkstraResult is the response from /graph/:schema/dijkstra. Cost is nil
// when Dst is unreachable from Src under the supplied weight function,
// otherwise it points to the total weight along the cheapest path.
type DijkstraResult struct {
	Cost *float64 `json:"cost"`
}

// AskResponse is the response from POST /v1/tenants/:t/ask. Rows is the
// engine-evaluated result of the natural-language plan; Cache is the
// cache disposition ("hit", "miss", "skip") and Plan is the executed
// plan when the caller passed show_plan=true.
type AskResponse struct {
	Rows  []map[string]any `json:"rows"`
	Cache string           `json:"cache"`
	Plan  json.RawMessage  `json:"plan,omitempty"`
}

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
}

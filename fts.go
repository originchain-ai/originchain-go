package originchain

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// FTSIndex indexes req.Text under (table, field, req.DocID). Per-tenant
// per-field inverted index; subsequent calls with the same DocID
// overwrite. Idempotent on identical (DocID, Text) pairs.
func (c *Client) FTSIndex(ctx context.Context, table, field string, req FTSIndexRequest) error {
	path := fmt.Sprintf("/v1/tenants/%s/fts/%s/%s", c.tenant, table, field)
	return c.request(ctx, "POST", path, nil, req, nil)
}

// FTSSearch runs a full-text search against the per-tenant inverted index
// for (table, field).
//
// Mode controls the matching strategy - see [FTSSearchRequest] for the
// three options. Boolean / phrase results have Score == 0.0; bm25
// results carry the BM25 ranking weight.
//
// The wire format returns []string for boolean / phrase modes and
// []{doc_id, score} for bm25; this method normalises both into [FTSHit]
// for a uniform shape.
func (c *Client) FTSSearch(ctx context.Context, table, field string, req FTSSearchRequest) ([]FTSHit, error) {
	q := url.Values{}
	q.Set("q", req.Q)
	if req.Mode != "" {
		q.Set("mode", req.Mode)
	}
	if req.K > 0 {
		q.Set("k", strconv.Itoa(req.K))
	}

	path := fmt.Sprintf("/v1/tenants/%s/fts/%s/%s", c.tenant, table, field)

	// The response is a tagged-union by mode. Decode into a permissive
	// shape and dispatch on the first element.
	var raw json.RawMessage
	if err := c.request(ctx, "GET", path, q, nil, &raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return []FTSHit{}, nil
	}

	// Try ranked-hit shape first.
	var ranked []FTSHit
	if err := json.Unmarshal(raw, &ranked); err == nil && len(ranked) > 0 && ranked[0].DocID != "" {
		return ranked, nil
	}

	// Fall back to plain doc_id list.
	var ids []string
	if err := json.Unmarshal(raw, &ids); err == nil {
		hits := make([]FTSHit, len(ids))
		for i, id := range ids {
			hits[i] = FTSHit{DocID: id}
		}
		return hits, nil
	}

	// Empty array literal.
	return []FTSHit{}, nil
}

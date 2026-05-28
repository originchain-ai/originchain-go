package originchain

import (
	"context"
	"fmt"
)

// VectorMode is the recall/latency profile selector for [Client.VectorTopK].
type VectorMode string

const (
	// ModeFast favours latency over recall. Smaller candidate set, fewer
	// distance computations.
	ModeFast VectorMode = "fast"
	// ModeHighRecall favours recall over latency. The server default
	// when [VectorTopKRequest.Mode] is unset.
	ModeHighRecall VectorMode = "high_recall"
)

// VectorPut upserts a single vector under (table, req.ID). Dim must match
// the table's configured dimensionality on every put - the substrate
// validates it server-side.
//
// Metric is one of "cosine" / "dot" / "l2" and is locked at the first
// put for a table; changing it later returns 400.
func (c *Client) VectorPut(ctx context.Context, table string, req VectorPutRequest) error {
	path := fmt.Sprintf("/v1/tenants/%s/vector/%s/put", c.tenant, table)
	return c.request(ctx, "POST", path, nil, req, nil)
}

// VectorTopK runs an approximate-nearest-neighbour search over a vector
// table. Mode picks the recall/latency profile (see [ModeFast] /
// [ModeHighRecall]); when omitted the server defaults to high_recall.
//
// req.Filter is a metadata equality filter - non-empty filters force an
// HNSW + post-filter codepath server-side.
func (c *Client) VectorTopK(ctx context.Context, table string, req VectorTopKRequest) ([]VectorHit, error) {
	path := fmt.Sprintf("/v1/tenants/%s/vector/%s/topk", c.tenant, table)
	var hits []VectorHit
	if err := c.request(ctx, "POST", path, nil, req, &hits); err != nil {
		return nil, err
	}
	return hits, nil
}

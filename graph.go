package originchain

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// Graph groups the five graph endpoints under
// /v1/tenants/:t/graph/:schema/{neighbors,reverse,bfs,path,dijkstra}.
// Get an instance via [Client.Graph]; do not construct directly.
type Graph struct {
	c *Client
}

// Graph returns the graph-namespace methods. The receiver is reused
// across calls; it holds no per-call state.
func (c *Client) Graph() *Graph { return c.graph }

// Neighbors returns the one-hop forward neighbours of req.PK along
// req.Rel. Each [Neighbor] has Depth == 1.
func (g *Graph) Neighbors(ctx context.Context, schema string, req NeighborsRequest) ([]Neighbor, error) {
	return g.oneHop(ctx, schema, "neighbors", req)
}

// ReverseNeighbors returns the one-hop reverse (in-edge) neighbours of
// req.PK along req.Rel.
func (g *Graph) ReverseNeighbors(ctx context.Context, schema string, req NeighborsRequest) ([]Neighbor, error) {
	return g.oneHop(ctx, schema, "reverse", req)
}

func (g *Graph) oneHop(ctx context.Context, schema, op string, req NeighborsRequest) ([]Neighbor, error) {
	q := url.Values{}
	q.Set("rel", req.Rel)
	q.Set("pk", req.PK)
	path := fmt.Sprintf("/v1/tenants/%s/graph/%s/%s", g.c.tenant, schema, op)

	// Engine returns a flat []string of PKs.
	var pks []string
	if err := g.c.request(ctx, "GET", path, q, nil, &pks); err != nil {
		return nil, err
	}
	out := make([]Neighbor, len(pks))
	for i, pk := range pks {
		out[i] = Neighbor{PK: pk, Depth: 1}
	}
	return out, nil
}

// BFS runs a breadth-first traversal up to req.MaxDepth hops along
// req.Rel from req.PK. The returned [BFSHit]s carry the BFS distance.
func (g *Graph) BFS(ctx context.Context, schema string, req BFSRequest) ([]BFSHit, error) {
	q := url.Values{}
	q.Set("rel", req.Rel)
	q.Set("pk", req.PK)
	if req.MaxDepth > 0 {
		q.Set("max_depth", strconv.Itoa(req.MaxDepth))
	}
	path := fmt.Sprintf("/v1/tenants/%s/graph/%s/bfs", g.c.tenant, schema)
	var hits []BFSHit
	if err := g.c.request(ctx, "GET", path, q, nil, &hits); err != nil {
		return nil, err
	}
	return hits, nil
}

// Path checks whether req.Dst is reachable from req.Src along req.Rel
// within req.MaxDepth hops. The actual path is not materialised in v0;
// only [PathResult.Reachable] is meaningful.
func (g *Graph) Path(ctx context.Context, schema string, req PathRequest) (*PathResult, error) {
	q := url.Values{}
	q.Set("rel", req.Rel)
	q.Set("src", req.Src)
	q.Set("dst", req.Dst)
	if req.MaxDepth > 0 {
		q.Set("max_depth", strconv.Itoa(req.MaxDepth))
	}
	path := fmt.Sprintf("/v1/tenants/%s/graph/%s/path", g.c.tenant, schema)
	var res PathResult
	if err := g.c.request(ctx, "GET", path, q, nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// Dijkstra finds the cheapest weighted path from req.Src to req.Dst
// along req.Rel under req.Weights.
//
// Weights is JSON-encoded into the weights_json query parameter — the
// engine reads it server-side rather than from a body, because the
// dijkstra endpoint is GET-shaped to keep traversals cacheable.
//
// [DijkstraResult.Cost] is nil when Dst is unreachable.
func (g *Graph) Dijkstra(ctx context.Context, schema string, req DijkstraRequest) (*DijkstraResult, error) {
	weights := req.Weights
	if weights == nil {
		weights = map[string]float64{}
	}
	weightsJSON, err := json.Marshal(weights)
	if err != nil {
		return nil, fmt.Errorf("originchain: encode dijkstra weights: %w", err)
	}

	q := url.Values{}
	q.Set("rel", req.Rel)
	q.Set("src", req.Src)
	q.Set("dst", req.Dst)
	q.Set("weights_json", string(weightsJSON))
	path := fmt.Sprintf("/v1/tenants/%s/graph/%s/dijkstra", g.c.tenant, schema)

	var res DijkstraResult
	if err := g.c.request(ctx, "GET", path, q, nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

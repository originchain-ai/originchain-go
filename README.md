# originchain-go

Official Go client for [OriginChain](https://originchain.ai) - managed database for AI applications.

> Other languages: Python → [`originchain`](https://pypi.org/project/originchain/) · TypeScript / JS → [`@originchain/sdk`](https://www.npmjs.com/package/@originchain/sdk) · raw HTTP → [originchain.ai/docs](https://originchain.ai/docs).

```
go get github.com/originchain-ai/originchain-go
```

Requires Go **1.21+**.

## Quickstart

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"

    "github.com/originchain-ai/originchain-go"
)

func main() {
    oc := originchain.NewClient(originchain.Config{
        BaseURL: "https://your-tenant.ap-south-1.db.originchain.ai",
        Bearer:  os.Getenv("OC_BEARER"),
    })

    rows, err := oc.SQL(context.Background(),
        "SELECT id, email FROM shop.customers LIMIT 10")
    if err != nil {
        log.Fatal(err)
    }
    for _, r := range rows.Rows {
        fmt.Println(r["id"], r["email"])
    }
}
```

The tenant ID is auto-derived from the leftmost DNS label of `BaseURL`. Pass `Config.Tenant` explicitly if you front the engine through a custom gateway whose hostname doesn't match the tenant ID.

## SQL

`SQL` returns a `*SQLResponse`, an internally-tagged union. Branch on `Kind`
against the `Kind*` constants before reading any other member - the engine
answers SELECT, INSERT, UPDATE, DELETE, EXPLAIN, transaction control and every
DDL statement through this one endpoint:

```go
resp, err := oc.SQL(ctx, "EXPLAIN ANALYZE SELECT * FROM shop.customers")
switch resp.Kind {
case originchain.KindSelect:
    for _, r := range resp.Rows { fmt.Println(r) }
case originchain.KindExplain:
    fmt.Println(resp.Plan, resp.Stats)
case originchain.KindInsert:
    fmt.Println(resp.Inserted, "rows inserted")
case originchain.KindDelete:
    // PK is nil for a scan-predicate delete - only the
    // `WHERE <pk> = <literal>` fastpath carries a row key.
    fmt.Println(resp.RowsAffected, resp.PK)
}
```

Pointer members are `nil` when the engine omitted them, which is not the same
as zero. Bind `$1`, `$2`, ... placeholders by passing params positionally:
`oc.SQL(ctx, "SELECT * FROM t WHERE id = $1", "c-1")`.

## Vector

```go
hits, err := oc.VectorTopK(ctx, "embeds", originchain.VectorTopKRequest{
    Query:  embedding,            // []float32
    K:      10,
    Dim:    1536,
    Metric: "cosine",
    Mode:   originchain.ModeHighRecall,
})
```

`Mode` selects the recall/latency profile - `ModeFast` favours latency, `ModeHighRecall` favours recall. Omit it to let the server default to high-recall.

`Metric` is one of the `Metric*` constants: `MetricCosine` (the default),
`MetricDot`, `MetricL2`, `MetricManhattan` (alias `MetricL1`). The engine
rejects anything else with a 400 - `"euclidean"`, `"inner_product"` and
`"ip"` are **not** accepted.

## Full-text search

```go
hits, err := oc.FTSSearch(ctx, "docs", "body", originchain.FTSSearchRequest{
    Q:    "originchain release",
    Mode: "bm25",
    K:    20,
})
```

`Mode` is one of `boolean` (default), `bm25`, or `phrase`. Boolean / phrase results have `Score == 0.0`; bm25 results carry the BM25 ranking weight.

## Graph

```go
res, err := oc.Graph().Dijkstra(ctx, "social", originchain.DijkstraRequest{
    Rel: "road",
    Src: "warehouse",
    Dst: "customer",
    // PER-EDGE weights, keyed "<from_pk>|<to_pk>".
    Weights: map[string]float64{
        originchain.EdgeWeightKey("warehouse", "depot"):  1.0,
        originchain.EdgeWeightKey("depot", "customer"):   0.25,
    },
})
if res.Cost != nil {
    fmt.Printf("cheapest route costs %.2f\n", *res.Cost)
}
```

`Weights` is a per-edge map, **not** a map of relation or column names. The
engine skips any edge the map doesn't cover, so a wrongly-keyed map reports
every destination as unreachable (`Cost == nil`) instead of erroring. Build
keys with `originchain.EdgeWeightKey`.

`res.Cost` is `nil` when `Dst` is unreachable from `Src` under the supplied weight function.

The other graph endpoints - `Neighbors`, `ReverseNeighbors`, `BFS`, `Path` - share the same shape. `BFS` and `Path` default to `MaxDepth: 3` server-side when you leave it zero.

## Natural-language ask

```go
resp, err := oc.Ask(ctx, "which customer placed the largest order this week?")
fmt.Println(resp.Cache, resp.Rows)
```

## Errors

Every method returns `error` on non-2xx HTTP responses. The error always carries an `*APIError`; 402 responses with the canonical add-on envelope additionally satisfy `*AddonRequiredError`. Use `errors.As`:

```go
_, err := oc.VectorTopK(ctx, "embeds", req)

var addonErr *originchain.AddonRequiredError
if errors.As(err, &addonErr) {
    // Render an "Enable <Name> ($X/mo)" CTA.
    fmt.Printf("enable %s ($%.0f/mo): %s\n",
        addonErr.Name, addonErr.MonthlyUSD, addonErr.PurchaseURL)
}

var apiErr *originchain.APIError
if errors.As(err, &apiErr) {
    switch apiErr.Status {
    case 401, 403:
        // wrong bearer
    case 404:
        // schema or row missing
    case 429:
        // rate-limited; back off and retry
    }
}
```

Helper sugar for the common case:

```go
if e := originchain.AsAPIError(err); e != nil {
    log.Printf("HTTP %d: %s", e.Status, e.Message)
}
```

## Engine compatibility

| SDK version | Engine min | Engine max |
| --- | --- | --- |
| `0.4.0`     | `1.0.0`    | `1.x`      |

The SDK speaks the v1 HTTP API. Major engine bumps (2.x) will be accompanied by a major SDK bump.

## Custom HTTP client

`Config.HTTP` overrides the default `&http.Client{Timeout: 30 * time.Second}` - useful for plugging in custom transports, retry middleware, or per-request observability.

```go
oc := originchain.NewClient(originchain.Config{
    BaseURL: endpoint,
    Bearer:  token,
    HTTP: &http.Client{
        Timeout:   60 * time.Second,
        Transport: &myInstrumentedTransport{},
    },
})
```

## License

MIT License. (c) 2026 Silicoyn Technologies Pvt Ltd. See `LICENSE`.

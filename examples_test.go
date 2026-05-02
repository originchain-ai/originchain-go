package originchain_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/originchain-ai/originchain-go"
)

// stubServer is a tiny mock used only in examples so go test can run them
// without hitting a real engine. Callers in production replace BaseURL
// with their tenant's HTTPS endpoint.
func stubServer(handler http.HandlerFunc) *httptest.Server {
	return httptest.NewServer(handler)
}

func ExampleNewClient() {
	oc := originchain.NewClient(originchain.Config{
		BaseURL: "https://my-tenant.ap-south-1.db.originchain.ai",
		Bearer:  os.Getenv("OC_BEARER"),
	})
	fmt.Println(oc.Tenant())
	// Output: my-tenant
}

func ExampleClient_SQL() {
	srv := stubServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"kind":"select","rows":[{"id":"1","email":"a@example.com"}]}`)
	})
	defer srv.Close()

	oc := originchain.NewClient(originchain.Config{
		BaseURL: srv.URL,
		Bearer:  "demo",
		Tenant:  "demo",
	})

	resp, err := oc.SQL(context.Background(), "SELECT id, email FROM shop.customers LIMIT 10")
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	for _, row := range resp.Rows {
		fmt.Println(row["id"], row["email"])
	}
	// Output: 1 a@example.com
}

func ExampleClient_SQLOne() {
	srv := stubServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"kind":"select","rows":[{"name":"Acme"}]}`)
	})
	defer srv.Close()

	oc := originchain.NewClient(originchain.Config{BaseURL: srv.URL, Bearer: "x", Tenant: "t"})
	row, _ := oc.SQLOne(context.Background(), "SELECT name FROM shop.customers WHERE id = 1")
	fmt.Println(row["name"])
	// Output: Acme
}

func ExampleClient_VectorPut() {
	srv := stubServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	})
	defer srv.Close()

	oc := originchain.NewClient(originchain.Config{BaseURL: srv.URL, Bearer: "x", Tenant: "t"})
	err := oc.VectorPut(context.Background(), "embeds", originchain.VectorPutRequest{
		ID:        "doc-1",
		Embedding: []float32{0.1, 0.2, 0.3},
		Dim:       3,
		Metric:    "cosine",
	})
	fmt.Println(err)
	// Output: <nil>
}

func ExampleClient_VectorTopK() {
	srv := stubServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"id":"a","score":0.97}]`)
	})
	defer srv.Close()

	oc := originchain.NewClient(originchain.Config{BaseURL: srv.URL, Bearer: "x", Tenant: "t"})
	hits, _ := oc.VectorTopK(context.Background(), "embeds", originchain.VectorTopKRequest{
		Query: []float32{0.1, 0.2, 0.3},
		K:     5,
		Dim:   3,
		Mode:  originchain.ModeHighRecall,
	})
	fmt.Println(hits[0].ID, hits[0].Score)
	// Output: a 0.97
}

func ExampleClient_FTSIndex() {
	srv := stubServer(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	defer srv.Close()

	oc := originchain.NewClient(originchain.Config{BaseURL: srv.URL, Bearer: "x", Tenant: "t"})
	err := oc.FTSIndex(context.Background(), "docs", "body", originchain.FTSIndexRequest{
		DocID: "blog-1",
		Text:  "OriginChain ships v1.0",
	})
	fmt.Println(err)
	// Output: <nil>
}

func ExampleClient_FTSSearch() {
	srv := stubServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"doc_id":"blog-1","score":3.2}]`)
	})
	defer srv.Close()

	oc := originchain.NewClient(originchain.Config{BaseURL: srv.URL, Bearer: "x", Tenant: "t"})
	hits, _ := oc.FTSSearch(context.Background(), "docs", "body", originchain.FTSSearchRequest{
		Q:    "OriginChain",
		Mode: "bm25",
		K:    10,
	})
	fmt.Println(hits[0].DocID)
	// Output: blog-1
}

func ExampleGraph_Neighbors() {
	srv := stubServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `["alice","bob"]`)
	})
	defer srv.Close()

	oc := originchain.NewClient(originchain.Config{BaseURL: srv.URL, Bearer: "x", Tenant: "t"})
	out, _ := oc.Graph().Neighbors(context.Background(), "social", originchain.NeighborsRequest{
		Rel: "follows",
		PK:  "carol",
	})
	for _, n := range out {
		fmt.Println(n.PK)
	}
	// Output:
	// alice
	// bob
}

func ExampleGraph_ReverseNeighbors() {
	srv := stubServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `["dan"]`)
	})
	defer srv.Close()

	oc := originchain.NewClient(originchain.Config{BaseURL: srv.URL, Bearer: "x", Tenant: "t"})
	out, _ := oc.Graph().ReverseNeighbors(context.Background(), "social", originchain.NeighborsRequest{
		Rel: "follows", PK: "carol",
	})
	fmt.Println(out[0].PK)
	// Output: dan
}

func ExampleGraph_BFS() {
	srv := stubServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"pk":"a","depth":1}]`)
	})
	defer srv.Close()

	oc := originchain.NewClient(originchain.Config{BaseURL: srv.URL, Bearer: "x", Tenant: "t"})
	hits, _ := oc.Graph().BFS(context.Background(), "social", originchain.BFSRequest{
		Rel: "follows", PK: "carol", MaxDepth: 3,
	})
	fmt.Println(hits[0].PK, hits[0].Depth)
	// Output: a 1
}

func ExampleGraph_Path() {
	srv := stubServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"reachable":true}`)
	})
	defer srv.Close()

	oc := originchain.NewClient(originchain.Config{BaseURL: srv.URL, Bearer: "x", Tenant: "t"})
	res, _ := oc.Graph().Path(context.Background(), "social", originchain.PathRequest{
		Rel: "follows", Src: "a", Dst: "b",
	})
	fmt.Println(res.Reachable)
	// Output: true
}

func ExampleGraph_Dijkstra() {
	srv := stubServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"cost":4.5}`)
	})
	defer srv.Close()

	oc := originchain.NewClient(originchain.Config{BaseURL: srv.URL, Bearer: "x", Tenant: "t"})
	res, _ := oc.Graph().Dijkstra(context.Background(), "social", originchain.DijkstraRequest{
		Rel:     "road",
		Src:     "a",
		Dst:     "b",
		Weights: map[string]float64{"distance": 1.0},
	})
	fmt.Println(*res.Cost)
	// Output: 4.5
}

func ExampleClient_Ask() {
	srv := stubServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"rows":[{"id":"1"}],"cache":"miss"}`)
	})
	defer srv.Close()

	oc := originchain.NewClient(originchain.Config{BaseURL: srv.URL, Bearer: "x", Tenant: "t"})
	resp, _ := oc.Ask(context.Background(), "top customers by revenue")
	fmt.Println(resp.Cache, len(resp.Rows))
	// Output: miss 1
}

// Example_addonRequired demonstrates the typed 402 error: an
// AddonRequiredError carries the add-on slug, display name, monthly
// price, and a purchase URL the UI can deep-link to.
func Example_addonRequired() {
	srv := stubServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(402)
		_, _ = io.WriteString(w, `{
			"error":"addon_required",
			"addon":"vector-search",
			"name":"Vector Search",
			"monthly_usd":49,
			"preview":false,
			"purchase_url":"https://app.originchain.ai/billing/addons?enable=vector-search",
			"msg":"Enable Vector Search to use this endpoint."
		}`)
	})
	defer srv.Close()

	oc := originchain.NewClient(originchain.Config{BaseURL: srv.URL, Bearer: "x", Tenant: "t"})
	_, err := oc.VectorTopK(context.Background(), "embeds", originchain.VectorTopKRequest{
		Query: []float32{0.1}, K: 1, Dim: 1,
	})
	var addonErr *originchain.AddonRequiredError
	if errors.As(err, &addonErr) {
		fmt.Printf("enable %s ($%.0f/mo)\n", addonErr.Name, addonErr.MonthlyUSD)
	}
	// Output: enable Vector Search ($49/mo)
}

// Example_multiShape exercises SQL → Vector → Ask in sequence so callers
// can see what a multi-modality workflow looks like end-to-end.
func Example_multiShape() {
	srv := stubServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tenants/t/sql":
			_, _ = io.WriteString(w, `{"kind":"select","rows":[{"id":"1"}]}`)
		case r.URL.Path == "/v1/tenants/t/vector/embeds/topk":
			_, _ = io.WriteString(w, `[{"id":"1","score":0.9}]`)
		case r.URL.Path == "/v1/tenants/t/ask":
			_, _ = io.WriteString(w, `{"rows":[{"id":"1"}],"cache":"hit"}`)
		default:
			http.NotFound(w, r)
		}
	})
	defer srv.Close()

	oc := originchain.NewClient(originchain.Config{BaseURL: srv.URL, Bearer: "x", Tenant: "t"})
	ctx := context.Background()

	rows, _ := oc.SQL(ctx, "SELECT id FROM shop.customers LIMIT 1")
	hits, _ := oc.VectorTopK(ctx, "embeds", originchain.VectorTopKRequest{
		Query: []float32{0.1, 0.2}, K: 1, Dim: 2,
		Mode: originchain.ModeHighRecall,
	})
	ans, _ := oc.Ask(ctx, "show me the top customer")

	out := map[string]any{
		"sql_kind":  rows.Kind,
		"hit_id":    hits[0].ID,
		"ask_cache": ans.Cache,
	}
	b, _ := json.Marshal(out)
	fmt.Println(string(b))
	// Output: {"ask_cache":"hit","hit_id":"1","sql_kind":"select"}
}

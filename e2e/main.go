// Command e2e exercises every originchain-go feature against a live engine.
// It is a CI/manual smoke test, not part of the importable SDK surface.
//
// Guarded: if OC_BASE is unset (e.g. a fork PR without secrets) it exits 0
// without running, so it never fails a build that simply lacks credentials.
//
//	OC_BASE=https://<tenant>.<region>.db.originchain.ai \
//	OC_BEARER=<bearer> OC_TENANT=<tenant-ulid> [OC_NS=goshop] go run ./e2e
//
// Graph uses a self-relation FK (the real engine model): the edge runs from a
// row's PK to the value in from_col.
package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	oc "github.com/originchain-ai/originchain-go"
)

var ns = envOr("OC_NS", "goshop")

var results [][3]string // name, "PASS"/"FAIL", detail

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func step(name string, wantFail bool, fn func() (any, error)) {
	v, err := fn()
	if err != nil {
		ok := "FAIL"
		if wantFail {
			ok = "PASS"
		}
		msg := err.Error()
		if len(msg) > 130 {
			msg = msg[:130]
		}
		results = append(results, [3]string{name, ok, msg})
		return
	}
	ok := "PASS"
	if wantFail {
		ok = "FAIL"
	}
	d := fmt.Sprintf("%v", v)
	if len(d) > 90 {
		d = d[:90]
	}
	results = append(results, [3]string{name, ok, d})
}

func productsTOML() string {
	return `version = 1
namespace = "` + ns + `"
table = "products"
primary_key = ["id"]
extractions = []
foreign_keys = []
check_constraints = []
triggers = []

[[columns]]
name = "id"
ty = "str"
required = true

[[columns]]
name = "name"
ty = "str"

[[columns]]
name = "price"
ty = "f64"

[[columns]]
name = "description"
ty = "str"

[[columns]]
name = "related_to"
ty = "str"

[[relations]]
name = "related"
from_col = "related_to"
bidirectional = true

[relations.target]
namespace = "` + ns + `"
table = "products"
pk = "id"
`
}

// registerSchema uses raw net/http: the Go SDK has no schema-register method.
func registerSchema(base, bearer, tenant, toml string) (any, error) {
	url := strings.TrimRight(base, "/") + "/v1/tenants/" + tenant + "/schemas"
	req, _ := http.NewRequest("POST", url, bytes.NewReader([]byte(toml)))
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "text/plain")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("register %d: %s", resp.StatusCode, strings.TrimSpace(buf.String()))
	}
	return strings.TrimSpace(buf.String()), nil
}

func main() {
	base := os.Getenv("OC_BASE")
	bearer := os.Getenv("OC_BEARER")
	tenant := os.Getenv("OC_TENANT")
	if base == "" {
		fmt.Println("OC_BASE unset — skipping live E2E (set OC_BASE/OC_BEARER/OC_TENANT to run).")
		return
	}
	ctx := context.Background()

	// Config.Tenant set explicitly — the recommended, free-tier-safe path.
	c := oc.NewClient(oc.Config{BaseURL: base, Bearer: bearer, Tenant: tenant})
	vec := []float32{0.1, 0.2, 0.1, 0.0, 0.3, 0.1, 0.2, 0.0}
	P := ns + ".products"

	step("registerSchema(products)", false, func() (any, error) {
		return registerSchema(base, bearer, tenant, productsTOML())
	})
	step("SQL INSERT p1", false, func() (any, error) {
		return c.SQL(ctx, "INSERT INTO "+P+" (id,name,price,description,related_to) VALUES ('p1','Widget',9.99,'a small blue widget for testing','p2')")
	})
	step("SQL INSERT p2", false, func() (any, error) {
		return c.SQL(ctx, "INSERT INTO "+P+" (id,name,price,description) VALUES ('p2','Gadget',2.5,'a tiny red gadget gizmo')")
	})
	step("SQL SELECT", false, func() (any, error) {
		return c.SQL(ctx, "SELECT id,name,price FROM "+P+" WHERE price > 5")
	})
	step("SQLOne COUNT", false, func() (any, error) {
		return c.SQLOne(ctx, "SELECT COUNT(*) FROM "+P)
	})
	step("VectorPut", false, func() (any, error) {
		return nil, c.VectorPut(ctx, P, oc.VectorPutRequest{ID: "p1", Embedding: vec, Dim: len(vec)})
	})
	step("VectorTopK", false, func() (any, error) {
		return c.VectorTopK(ctx, P, oc.VectorTopKRequest{Query: vec, K: 5, Dim: len(vec), Metric: "cosine"})
	})
	step("FTSIndex", false, func() (any, error) {
		return nil, c.FTSIndex(ctx, P, "description", oc.FTSIndexRequest{DocID: "p1", Text: "a small blue widget for testing"})
	})
	step("FTSSearch", false, func() (any, error) {
		return c.FTSSearch(ctx, P, "description", oc.FTSSearchRequest{Q: "widget", Mode: "bm25", K: 5})
	})
	step("Graph.Neighbors ->[p2]", false, func() (any, error) {
		nbrs, err := c.Graph().Neighbors(ctx, P, oc.NeighborsRequest{Rel: "related", PK: "p1"})
		if err != nil {
			return nil, err
		}
		if len(nbrs) != 1 || nbrs[0].PK != "p2" {
			return nil, fmt.Errorf("expected [p2], got %v", nbrs)
		}
		return nbrs, nil
	})
	step("Ask", false, func() (any, error) {
		return c.Ask(ctx, "how many products cost more than 5")
	})
	step("Usage", false, func() (any, error) {
		return c.Usage(ctx)
	})
	step("SQL SELECT 1 (want-fail)", true, func() (any, error) {
		return c.SQL(ctx, "SELECT 1")
	})

	fmt.Printf("\n=== GO SDK %s E2E (ns=%s) ===\n", oc.Version, ns)
	np := 0
	for _, r := range results {
		fmt.Printf("%-4s %-28s %s\n", r[1], r[0], r[2])
		if r[1] == "PASS" {
			np++
		}
	}
	fmt.Printf("=== %d/%d passed ===\n", np, len(results))
	if np != len(results) {
		os.Exit(1)
	}
}

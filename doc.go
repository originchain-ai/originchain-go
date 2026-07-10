// Package originchain is the official Go client for OriginChain - a managed
// database for AI applications. It speaks the v1 HTTP API directly against
// a per-tenant engine endpoint.
//
// # Quickstart
//
//	package main
//
//	import (
//	    "context"
//	    "fmt"
//	    "log"
//	    "os"
//
//	    "github.com/originchain-ai/originchain-go"
//	)
//
//	func main() {
//	    oc := originchain.NewClient(originchain.Config{
//	        BaseURL: "https://your-tenant.ap-south-1.db.originchain.ai",
//	        Bearer:  os.Getenv("OC_BEARER"),
//	    })
//
//	    rows, err := oc.SQL(context.Background(),
//	        "SELECT id, email FROM shop.customers LIMIT 10")
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//	    for _, r := range rows.Rows {
//	        fmt.Println(r["id"], r["email"])
//	    }
//	}
//
// # Engine compatibility
//
// This SDK is built against engine versions in the range [1.0.0, 1.x]. It
// will run against newer engines but unknown response fields are dropped
// rather than surfaced - bump the SDK to pick them up.
//
//	const EngineMin = "1.0.0"
//	const EngineMax = "1.x"
//
// # Error handling
//
// All API failures return an error that wraps an [*APIError]. 402 responses
// with the canonical addon-required envelope additionally satisfy
// [*AddonRequiredError]. Use errors.As to branch:
//
//	var addonErr *originchain.AddonRequiredError
//	if errors.As(err, &addonErr) {
//	    fmt.Printf("enable %s ($%.0f/mo): %s\n",
//	        addonErr.Name, addonErr.MonthlyUSD, addonErr.PurchaseURL)
//	}
//
// See the package examples for one-per-method runnable snippets.
package originchain

// Version is the semantic version of this SDK. Sent on every request as
// part of the User-Agent header.
const Version = "0.2.0"

// EngineMin is the minimum engine version this SDK is known to be
// compatible with. Older engines may reject requests this SDK sends.
const EngineMin = "1.0.0"

// EngineMax is the maximum engine version line this SDK is known to be
// compatible with. The "1.x" notation means any 1.y.z release.
const EngineMax = "1.x"

package originchain

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestUsage_DecodesNeutralConfiguration verifies GET /usage decodes the
// neutral configuration and never surfaces a weather codename.
func TestUsage_DecodesNeutralConfiguration(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assertCommonHeaders(t, r)
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/usage") {
			t.Errorf("path = %s, want .../usage", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"tenant":"test-tenant",
			"tier":"standard",
			"configuration":{"slug":"standard","label":"4 vCPU / 16 GB, HA","vcpu":4,"ram_gb":16,"storage_gb":100,"ha":true,"monthly_price":699},
			"used":{"store_keys":42},
			"schemas":[]
		}`)
	})

	u, err := c.Usage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Neutral slug, never the weather codename.
	if u.Tier != "standard" {
		t.Errorf("Tier = %q, want standard", u.Tier)
	}
	if u.Configuration == nil || u.Configuration.Slug != "standard" {
		t.Fatalf("Configuration = %+v, want slug=standard", u.Configuration)
	}
	if u.Configuration.VCPU == nil || *u.Configuration.VCPU != 4 {
		t.Errorf("VCPU = %v, want 4", u.Configuration.VCPU)
	}
	if !u.Configuration.HA {
		t.Errorf("HA = false, want true")
	}
	if u.Configuration.MonthlyPrice == nil || *u.Configuration.MonthlyPrice != 699 {
		t.Errorf("MonthlyPrice = %v, want 699", u.Configuration.MonthlyPrice)
	}
}

// TestUsage_LegacyModeOmitsConfiguration verifies legacy per-addon mode
// (no tier / configuration) decodes cleanly.
func TestUsage_LegacyModeOmitsConfiguration(t *testing.T) {
	_, c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"tenant":"test-tenant","used":{"store_keys":7},"schemas":[]}`)
	})

	u, err := c.Usage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if u.Tier != "" {
		t.Errorf("Tier = %q, want empty", u.Tier)
	}
	if u.Configuration != nil {
		t.Errorf("Configuration = %+v, want nil", u.Configuration)
	}
}

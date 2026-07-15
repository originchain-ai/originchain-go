package originchain

import (
	"context"
	"fmt"
)

// Usage reads GET /v1/tenants/:t/usage: live counters, the per-schema
// breakdown, and the tenant's compute Configuration.
//
// The response Tier is the neutral configuration slug
// (entry/standard/advanced/custom); the internal weather codename is
// never exposed. Prefer Configuration for the full spec + list price.
// Both are absent in legacy per-addon mode.
func (c *Client) Usage(ctx context.Context) (*TenantUsage, error) {
	path := fmt.Sprintf("/v1/tenants/%s/usage", c.tenant)
	var resp TenantUsage
	if err := c.request(ctx, "GET", path, nil, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

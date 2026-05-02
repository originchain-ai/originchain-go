package originchain

import (
	"context"
	"fmt"
)

// Ask runs a natural-language query against the substrate's planner. The
// engine compiles question into a typed plan, executes it, and returns
// the rows together with a cache disposition.
//
// This is a convenience wrapper for the most common shape — passing a
// schemas allowlist or show_plan is the engine's job in v1; for v0 the
// SDK keeps the surface small and lets callers drop down to SQL when
// they need fine control.
func (c *Client) Ask(ctx context.Context, question string) (*AskResponse, error) {
	body := map[string]any{"nl": question}
	path := fmt.Sprintf("/v1/tenants/%s/ask", c.tenant)
	var resp AskResponse
	if err := c.request(ctx, "POST", path, nil, body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

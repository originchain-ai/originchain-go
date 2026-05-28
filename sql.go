package originchain

import (
	"context"
	"encoding/json"
	"fmt"
)

// SQL executes a SQL statement against the substrate. The variadic params
// are reserved for future use - the v0 wire format takes the literal
// query string, and parameter binding lands in v1. Passing params today
// is accepted but ignored by the engine.
//
// Returns a [SQLResponse] with the discriminated-union shape: see the
// type's docstring for how to branch on Kind. Errors map per the package
// convention - APIError on non-2xx, AddonRequiredError on 402.
func (c *Client) SQL(ctx context.Context, query string, params ...any) (*SQLResponse, error) {
	body := map[string]any{"sql": query}
	if len(params) > 0 {
		body["params"] = params
	}

	// Decode into a permissive shape first because the wire format puts
	// rows directly inside a kind="select" envelope but row entries can
	// be either objects (typical) or scalars (when the projection picks
	// a single column).
	var raw struct {
		Kind   string            `json:"kind"`
		Rows   []json.RawMessage `json:"rows,omitempty"`
		Schema string            `json:"schema,omitempty"`
		PK     string            `json:"pk,omitempty"`
	}
	path := fmt.Sprintf("/v1/tenants/%s/sql", c.tenant)
	if err := c.request(ctx, "POST", path, nil, body, &raw); err != nil {
		return nil, err
	}

	resp := &SQLResponse{
		Kind:   raw.Kind,
		Schema: raw.Schema,
		PK:     raw.PK,
	}
	resp.Rows = make([]map[string]any, 0, len(raw.Rows))
	for _, r := range raw.Rows {
		var obj map[string]any
		if err := json.Unmarshal(r, &obj); err == nil && obj != nil {
			resp.Rows = append(resp.Rows, obj)
			continue
		}
		// Scalar projection - wrap as {"value": ...} to keep Rows uniform.
		var scalar any
		if err := json.Unmarshal(r, &scalar); err == nil {
			resp.Rows = append(resp.Rows, map[string]any{"value": scalar})
		}
	}
	return resp, nil
}

// SQLOne runs query as a SELECT and returns the first row, or nil when
// the result set is empty. Errors with [*APIError] of code
// "validation_failed" if the statement isn't a SELECT - there's no
// "first" of an INSERT or DELETE translation.
func (c *Client) SQLOne(ctx context.Context, query string, params ...any) (map[string]any, error) {
	resp, err := c.SQL(ctx, query, params...)
	if err != nil {
		return nil, err
	}
	if resp.Kind != "select" {
		return nil, &APIError{
			Status:  400,
			Code:    "validation_failed",
			Message: fmt.Sprintf("SQLOne expected SELECT, got %s", resp.Kind),
		}
	}
	if len(resp.Rows) == 0 {
		return nil, nil
	}
	return resp.Rows[0], nil
}

package originchain

import (
	"context"
	"fmt"
)

// SQL executes a SQL statement against the substrate. params binds the
// $1, $2, ... placeholders in query positionally (params[0] fills $1); the
// engine substitutes them at the AST level, never by string splicing. Pass
// none for a literal statement.
//
// Returns a [SQLResponse] - an internally-tagged union. Branch on
// [SQLResponse.Kind] against the Kind* constants before reading any other
// member; the engine answers SELECT, INSERT, UPDATE, DELETE, EXPLAIN,
// transaction control, and every DDL statement through this one endpoint.
// Errors map per the package convention - APIError on non-2xx,
// AddonRequiredError on 402.
func (c *Client) SQL(ctx context.Context, query string, params ...any) (*SQLResponse, error) {
	body := map[string]any{"sql": query}
	if len(params) > 0 {
		body["params"] = params
	}

	// SQLResponse.UnmarshalJSON owns the envelope decode, including the
	// scalar-row normalisation a single-column projection needs.
	path := fmt.Sprintf("/v1/tenants/%s/sql", c.tenant)
	var resp SQLResponse
	if err := c.request(ctx, "POST", path, nil, body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
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
	if resp.Kind != KindSelect {
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

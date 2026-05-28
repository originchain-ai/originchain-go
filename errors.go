package originchain

import "fmt"

// APIError is the canonical error returned by every Client method when the
// engine responds with a non-2xx status. It implements error and is safe to
// branch on with errors.As:
//
//	var apiErr *originchain.APIError
//	if errors.As(err, &apiErr) {
//	    if apiErr.Status == 401 { ... }
//	}
//
// Status holds the HTTP status code. Code is the engine-provided machine-
// readable identifier (e.g. "addon_required", "validation_failed") when one
// was present in the body, or "http_error" as a fallback. Message is the
// human-readable string the engine returned, falling back to the raw body.
type APIError struct {
	Status  int
	Code    string
	Message string
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.Code != "" && e.Code != "http_error" {
		return fmt.Sprintf("originchain: HTTP %d %s: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("originchain: HTTP %d: %s", e.Status, e.Message)
}

// AddonRequiredError is the typed error for HTTP 402 responses that carry
// the canonical addon-required envelope. It embeds [APIError] so existing
// errors.As(&apiErr) sites still match - they just don't see the addon
// fields unless they branch on the more specific type:
//
//	var addonErr *originchain.AddonRequiredError
//	if errors.As(err, &addonErr) {
//	    // render an "Enable <Name> ($X/mo)" CTA
//	}
//
// The 402 body shape is documented in oc-addon-entitlements-spec.md.
type AddonRequiredError struct {
	APIError
	// Addon is the machine-readable add-on slug (e.g. "vector-search").
	Addon string
	// Name is the human-readable add-on display name (e.g. "Vector Search").
	Name string
	// MonthlyUSD is the snapshot price in US dollars per month at the time
	// the engine emitted the 402.
	MonthlyUSD float64
	// Preview is true when the add-on is in preview and requires explicit
	// preview consent to enable.
	Preview bool
	// PurchaseURL is the canonical URL the customer can hit to enable the
	// add-on from the control plane UI.
	PurchaseURL string
}

// Error implements the error interface. The message reflects the addon-
// specific copy when present, otherwise the embedded APIError's text.
func (e *AddonRequiredError) Error() string {
	if e.Name != "" {
		return fmt.Sprintf(
			"originchain: HTTP 402 addon_required: %s requires the %s add-on ($%.0f/mo): %s",
			e.PurchaseURL, e.Name, e.MonthlyUSD, e.Message,
		)
	}
	return e.APIError.Error()
}

// Unwrap exposes the embedded *APIError so errors.As(err, &apiErr) and
// errors.Is(err, &APIError{Status: ...}) keep working when err is an
// *AddonRequiredError. Without this, the embedded value is invisible to
// the errors.As machinery.
func (e *AddonRequiredError) Unwrap() error {
	return &e.APIError
}

// Is supports errors.Is(err, &APIError{...}) by status when callers want a
// loose match - typical use is errors.As, but Is keeps idiomatic ergonomics.
func (e *APIError) Is(target error) bool {
	t, ok := target.(*APIError)
	if !ok {
		return false
	}
	if t.Status == 0 {
		return true
	}
	return t.Status == e.Status
}

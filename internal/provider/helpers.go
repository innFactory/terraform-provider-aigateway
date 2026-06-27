package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// strList wraps types.ListValueFrom for []string with the usual diag plumbing.
func strList(ctx context.Context, diags *diag.Diagnostics, in []string) types.List {
	v, d := types.ListValueFrom(ctx, types.StringType, in)
	diags.Append(d...)
	if d.HasError() {
		return types.ListNull(types.StringType)
	}
	return v
}

// optString returns the string value, or "" when null/unknown.
func optString(v types.String) string {
	if v.IsNull() || v.IsUnknown() {
		return ""
	}
	return v.ValueString()
}

// strOrNull maps "" → null, else a string value (keeps optional-computed attrs tidy).
func strOrNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// strPtr returns a pointer to the string value, or nil when null/unknown.
func strPtr(s types.String) *string {
	if s.IsNull() || s.IsUnknown() {
		return nil
	}
	v := s.ValueString()
	return &v
}

// int64Ptr returns a pointer to the int64 value, or nil when null/unknown.
func int64Ptr(v types.Int64) *int64 {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	x := v.ValueInt64()
	return &x
}

// boolPtr returns a pointer to the bool value, or nil when null/unknown.
func boolPtr(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	x := v.ValueBool()
	return &x
}

// parseJSONArray decodes a user-supplied JSON string into []any so it can be
// included as a real JSON array in the request body (not a quoted string).
// Returns an empty slice for an empty input string.
func parseJSONArray(s string) ([]any, error) {
	if s == "" {
		return []any{}, nil
	}
	var arr []any
	if err := json.Unmarshal([]byte(s), &arr); err != nil {
		return nil, fmt.Errorf("must be a valid JSON array: %w", err)
	}
	return arr, nil
}

// compactJSON normalises raw JSON to a compact single-line representation for
// stable state storage (avoids spurious diffs from whitespace).
func compactJSON(raw json.RawMessage) (string, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

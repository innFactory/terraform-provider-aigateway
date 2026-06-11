package provider

import (
	"context"

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

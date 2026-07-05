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

// ambiguousModelRefDetail expands a gateway 409 ("model name exists under
// multiple providers", gateway >= v0.16.16 resolves model routes doc-id-first
// and rejects ambiguous names) into an actionable message; every other error
// passes through unchanged. For resources that reference a model by the
// user-supplied model_id without storing the doc id themselves
// (fallback_chain, deployment_group, the model data source), the fix is to
// reference the model's server doc id (aigateway_model.<name>.id) — the
// gateway accepts it anywhere a model_id is expected.
func ambiguousModelRefDetail(err error) string {
	if isConflict(err) {
		return "referenced model name is ambiguous across providers — set model_id to the model's server doc id (aigateway_model.<name>.id, model_<uuid>) instead of the name: " + err.Error()
	}
	return err.Error()
}

// boolPtr returns a pointer to the bool value, or nil when null/unknown.
func boolPtr(v types.Bool) *bool {
	if v.IsNull() || v.IsUnknown() {
		return nil
	}
	x := v.ValueBool()
	return &x
}

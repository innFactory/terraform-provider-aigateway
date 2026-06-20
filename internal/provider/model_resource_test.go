package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// managed_by is Optional (NOT Computed). When the config sets it and the gateway
// echoes it back, apply() must preserve the value exactly.
func TestModelApplyPreservesManagedBy(t *testing.T) {
	r := &modelResource{}
	m := &modelResourceModel{ManagedBy: types.StringValue("companygpt-terraform")}
	a := &modelAPI{ID: "model_x", ModelID: "gpt-5.4", ManagedBy: "companygpt-terraform", Enabled: true}

	r.apply(m, a)

	if m.ManagedBy.ValueString() != "companygpt-terraform" {
		t.Errorf("managed_by must round-trip, got %q", m.ManagedBy.ValueString())
	}
}

// managed_by is Optional-only: when unset in config its planned value is null
// (known, not unknown). The gateway returns an empty string. apply() must leave
// the model value untouched (null) and must NOT force it to unknown — otherwise
// we'd reproduce the api_version inconsistency bug class.
func TestModelApplyLeavesUnsetManagedByNull(t *testing.T) {
	r := &modelResource{}
	m := &modelResourceModel{ManagedBy: types.StringNull()} // unset in config => null plan value
	a := &modelAPI{ID: "model_x", ModelID: "gpt-5.4", ManagedBy: "", Enabled: true}

	r.apply(m, a)

	if m.ManagedBy.IsUnknown() {
		t.Fatal("managed_by must not become unknown after apply")
	}
	if !m.ManagedBy.IsNull() {
		t.Errorf("managed_by should stay null when unset and gateway returns none, got %q", m.ManagedBy.ValueString())
	}
}

package provider

import (
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// The PATCH body must carry currency, user-max, default cost center and a
// managed revision. User-max unlimited sends null (the gateway double-option
// clears the per-user cap); a set user-max sends the microdollar value.
// Org-budget unlimited sends 0 (the gateway 0-sentinel convention).
func TestTenantPatchBodyMarshalsFull(t *testing.T) {
	userBudget := int64(50000000)
	body := tenantPatchBody{
		DefaultAllowedModels:    []string{"gpt-5.4"},
		OrgBudgetMicros:         0,
		Currency:                "EUR",
		DefaultUserBudgetMicros: &userBudget,
		DefaultCostCenterID:     "budget_companygpt",
		ManagedRevision:         "2026-06-21T10:00:00Z",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	want := `{"defaultAllowedModels":["gpt-5.4"],"orgBudgetLimitMicrodollars":0,"currency":"EUR","defaultUserBudgetMicrodollars":50000000,"defaultCostCenterId":"budget_companygpt","managedRevision":"2026-06-21T10:00:00Z"}`
	if got != want {
		t.Errorf("patch body mismatch\n got: %s\nwant: %s", got, want)
	}
}

// TestTenantPatchBodyUserUnlimitedSerialisesNull verifies that setting
// default_user_budget_unlimited=true emits "defaultUserBudgetMicrodollars":null
// (not 0). The gateway's double-option field treats null as "clear the cap";
// sending 0 would BLOCK all users (a zero-dollar per-user cap).
func TestTenantPatchBodyUserUnlimitedSerialisesNull(t *testing.T) {
	body := tenantPatchBody{
		DefaultAllowedModels:    []string{"gpt-4o"},
		OrgBudgetMicros:         0,
		Currency:                "USD",
		DefaultUserBudgetMicros: nil, // unlimited: nil → JSON null
		ManagedRevision:         "2026-06-21T10:00:00Z",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	want := `{"defaultAllowedModels":["gpt-4o"],"orgBudgetLimitMicrodollars":0,"currency":"USD","defaultUserBudgetMicrodollars":null,"managedRevision":"2026-06-21T10:00:00Z"}`
	if got != want {
		t.Errorf("patch body mismatch\n got: %s\nwant: %s", got, want)
	}
}

// Read must NOT overwrite the configured mutable fields from the gateway
// response (last-writer-wins: a dashboard edit must not be reverted). Only
// the singleton id is reconciled.
func TestTenantSettingsReadDoesNotRevertMutableFields(t *testing.T) {
	state := tenantSettingsResourceModel{
		Currency:                types.StringValue("EUR"),
		DefaultUserBudgetMicros: types.Int64Value(50000000),
		DefaultCostCenterID:     types.StringValue("budget_companygpt"),
	}
	// applyTenantRead simulates a gateway GET that reports DIFFERENT values (a
	// dashboard edit). Read must leave the planned/state values untouched.
	out := tenantAPI{
		Currency:                      "USD",
		DefaultUserBudgetMicrodollars: ptrInt64(999),
		DefaultCostCenterID:           "budget_other",
	}
	applyTenantRead(&state, &out)
	if state.Currency.ValueString() != "EUR" {
		t.Errorf("currency reverted to %q, want EUR", state.Currency.ValueString())
	}
	if state.DefaultUserBudgetMicros.ValueInt64() != 50000000 {
		t.Errorf("user max reverted to %d", state.DefaultUserBudgetMicros.ValueInt64())
	}
	if state.DefaultCostCenterID.ValueString() != "budget_companygpt" {
		t.Errorf("default cost center reverted to %q", state.DefaultCostCenterID.ValueString())
	}
}

func ptrInt64(v int64) *int64 { return &v }

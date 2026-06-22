package provider

import (
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// The create body must marshal to the gateway's camelCase shape, omitting the
// optional fields when null and emitting monthlyCap as a JSON string.
func TestCostCenterCreateBodyMarshalsFull(t *testing.T) {
	cap := "500.00"
	desc := "Marketing team"
	isOrg := true
	body := costCenterCreateBody{
		Name:        "marketing",
		Currency:    "EUR",
		Description: &desc,
		IsOrg:       &isOrg,
		MonthlyCap:  &cap,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	want := `{"name":"marketing","currency":"EUR","description":"Marketing team","isOrg":true,"monthlyCap":"500.00"}`
	if got != want {
		t.Errorf("create body mismatch\n got: %s\nwant: %s", got, want)
	}
}

// With only the required fields set, the optional fields must be omitted.
func TestCostCenterCreateBodyOmitsOptional(t *testing.T) {
	body := costCenterCreateBody{
		Name:     "ops",
		Currency: "USD",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	want := `{"name":"ops","currency":"USD"}`
	if got != want {
		t.Errorf("create body mismatch\n got: %s\nwant: %s", got, want)
	}
}

// Create defaults the currency to USD when the plan leaves it null/empty, and
// apply() must resolve the Optional+Computed currency to a known value.
func TestCostCenterApplyDefaultsCurrency(t *testing.T) {
	r := &costCenterResource{}
	m := &costCenterResourceModel{Currency: types.StringNull()}
	a := &costCenterAPI{ID: "budget_1", Name: "ops", Currency: ""}

	r.apply(m, a, "USD")

	if m.Currency.IsNull() || m.Currency.IsUnknown() {
		t.Fatal("currency must be known after apply")
	}
	if m.Currency.ValueString() != "USD" {
		t.Errorf("currency should default to USD, got %q", m.Currency.ValueString())
	}
}

// apply() must round-trip an explicit currency echoed by the gateway, and
// reflect monthly_cap when present.
func TestCostCenterApplyReflectsServerValues(t *testing.T) {
	r := &costCenterResource{}
	cap := "250.00"
	m := &costCenterResourceModel{
		Currency:   types.StringValue("EUR"),
		IsOrg:      types.BoolValue(true),
		MonthlyCap: types.StringValue("250.00"),
	}
	a := &costCenterAPI{ID: "budget_2", Name: "marketing", Currency: "EUR", IsOrg: true, MonthlyCap: &cap}

	r.apply(m, a, "EUR")

	if m.Currency.ValueString() != "EUR" {
		t.Errorf("currency must round-trip, got %q", m.Currency.ValueString())
	}
	if m.MonthlyCap.ValueString() != "250.00" {
		t.Errorf("monthly_cap must round-trip, got %q", m.MonthlyCap.ValueString())
	}
	if !m.IsOrg.ValueBool() {
		t.Errorf("is_org must round-trip true")
	}
}

// is_org is Optional-only: when unset in config its planned value is null.
// apply() must NOT force it to the server's false (would be an inconsistent
// result vs the plan).
func TestCostCenterApplyLeavesUnsetIsOrgNull(t *testing.T) {
	r := &costCenterResource{}
	m := &costCenterResourceModel{IsOrg: types.BoolNull()}
	a := &costCenterAPI{ID: "budget_3", Name: "ops", Currency: "USD", IsOrg: false}

	r.apply(m, a, "USD")

	if !m.IsOrg.IsNull() {
		t.Errorf("is_org should stay null when unset, got %v", m.IsOrg.ValueBool())
	}
}

// Create body marshals all the new budget fields in camelCase, omitting unset
// optionals; caps are JSON strings; fallbackChain is an array of budget ids.
func TestCostCenterCreateBodyFullBudget(t *testing.T) {
	monthly := "500.00"
	weekly := "200.00"
	daily := "50.00"
	mode := "per_user"
	agent := "agent_42"
	autoAdd := true
	body := costCenterCreateBody{
		Name:            "customer-a",
		Currency:        "EUR",
		Mode:            &mode,
		MonthlyCap:      &monthly,
		WeeklyCap:       &weekly,
		DailyCap:        &daily,
		AgentID:         &agent,
		AutoAddNewUsers: &autoAdd,
		FallbackChain:   []string{"budget_companygpt"},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	want := `{"name":"customer-a","currency":"EUR","mode":"per_user","monthlyCap":"500.00","weeklyCap":"200.00","dailyCap":"50.00","agentId":"agent_42","autoAddNewUsers":true,"fallbackChain":["budget_companygpt"]}`
	if got != want {
		t.Errorf("create body mismatch\n got: %s\nwant: %s", got, want)
	}
}

// The UPDATE body sends explicit null for a cleared cap (NOT omitempty), so the
// gateway double-option path clears it. A kept cap is a string; an unset cap in
// the model produces null too (clear) — Terraform distinguishes "leave" via not
// diffing, so null here is always "set to this value or clear".
func TestCostCenterUpdateBodyClearsCapWithNull(t *testing.T) {
	monthly := "100.00"
	body := costCenterUpdateBody{
		Name:       strp("customer-a"),
		MonthlyCap: &monthly, // set
		WeeklyCap:  nil,      // cleared → explicit null
		DailyCap:   nil,      // cleared → explicit null
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	want := `{"name":"customer-a","monthlyCap":"100.00","weeklyCap":null,"dailyCap":null}`
	if got != want {
		t.Errorf("update body mismatch\n got: %s\nwant: %s", got, want)
	}
}

func strp(s string) *string { return &s }

// A sub-limit create body marshals the scope as an internally-tagged object and
// caps as decimal strings, omitting unset optionals.
func TestSubLimitCreateBodyScopeTagging(t *testing.T) {
	cap := "50.00"
	weekly := "20.00"
	body := subLimitCreateBody{
		Scope:     subLimitScopeBody{Type: "provider", ProviderID: "provider_anthropic"},
		CapAmount: cap,
		WeeklyCap: &weekly,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	want := `{"scope":{"type":"provider","providerId":"provider_anthropic"},"capAmount":"50.00","weeklyCap":"20.00"}`
	if got != want {
		t.Errorf("sub-limit body mismatch\n got: %s\nwant: %s", got, want)
	}
}

// scopeKey produces a stable identity per sub-limit so reconcile can match
// desired against current regardless of server-assigned ids.
func TestSubLimitScopeKey(t *testing.T) {
	cases := []struct {
		m    subLimitModel
		want string
	}{
		{subLimitModel{ScopeType: types.StringValue("provider"), ScopeID: types.StringValue("provider_x")}, "provider:provider_x"},
		{subLimitModel{ScopeType: types.StringValue("model"), ScopeID: types.StringValue("gpt-5.4")}, "model:gpt-5.4"},
		{subLimitModel{ScopeType: types.StringValue("alias"), AliasName: types.StringValue("smart")}, "alias:smart"},
		{subLimitModel{ScopeType: types.StringValue("router"), ScopeID: types.StringValue("router_1")}, "router:router_1"},
	}
	for _, c := range cases {
		if got := c.m.scopeKey(); got != c.want {
			t.Errorf("scopeKey()=%q want %q", got, c.want)
		}
	}
}

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

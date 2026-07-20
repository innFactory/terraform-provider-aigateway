package provider

import (
	"encoding/json"
	"testing"
)

// The gateway may serialise a cost center's sub-limit caps as JSON numbers
// (rust_decimal `serde-float`) or strings. A refresh/plan must decode BOTH
// without failing — this reproduces the innfactory26 breakage
// (`json: cannot unmarshal number into ... capAmount of type string`).
func TestCostCenterAPIDecodesNumericSubLimitCaps(t *testing.T) {
	// capAmount as a bare number, dailyCap as a number, weeklyCap absent —
	// exactly what a serde-float gateway emits.
	raw := `{
		"id": "budget_1",
		"name": "companygpt",
		"currency": "EUR",
		"subLimits": [
			{"id": "s1", "scope": {"type": "provider", "providerId": "p_anthropic"}, "capAmount": 5, "dailyCap": 1.5, "weeklyCap": null}
		]
	}`
	var out costCenterAPI
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("numeric sub-limit caps must decode, got: %v", err)
	}
	if len(out.SubLimits) != 1 {
		t.Fatalf("expected 1 sub-limit, got %d", len(out.SubLimits))
	}
	sl := out.SubLimits[0]
	if sl.CapAmount.String() != "5" {
		t.Errorf("capAmount: want %q, got %q", "5", sl.CapAmount.String())
	}
	if sl.DailyCap == nil || sl.DailyCap.String() != "1.5" {
		t.Errorf("dailyCap: want %q, got %v", "1.5", sl.DailyCap)
	}
	if sl.WeeklyCap != nil {
		t.Errorf("weeklyCap null must decode to nil, got %v", sl.WeeklyCap)
	}
}

// String-form caps must keep working unchanged (the fixed gateway emits these).
func TestCostCenterAPIDecodesStringCaps(t *testing.T) {
	raw := `{
		"id": "b", "name": "cc", "currency": "USD",
		"monthlyCap": "500.00",
		"subLimits": [
			{"id": "s", "scope": {"type": "provider", "providerId": "p"}, "capAmount": "5.00", "dailyCap": null, "weeklyCap": "10.00"}
		]
	}`
	var out costCenterAPI
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("string caps must decode, got: %v", err)
	}
	if out.MonthlyCap == nil || out.MonthlyCap.String() != "500.00" {
		t.Errorf("monthlyCap: want %q, got %v", "500.00", out.MonthlyCap)
	}
	if out.SubLimits[0].CapAmount.String() != "5.00" {
		t.Errorf("capAmount: want %q, got %q", "5.00", out.SubLimits[0].CapAmount.String())
	}
	if out.SubLimits[0].WeeklyCap == nil || out.SubLimits[0].WeeklyCap.String() != "10.00" {
		t.Errorf("weeklyCap: want %q, got %v", "10.00", out.SubLimits[0].WeeklyCap)
	}
}

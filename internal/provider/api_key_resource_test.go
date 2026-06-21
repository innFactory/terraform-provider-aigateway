package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// The create body must include costCenterId (camelCase) when a cost center is
// configured on the key.
func TestAPIKeyCreateBodyIncludesCostCenterID(t *testing.T) {
	body := apiKeyCreateBody{
		Name:         "ci",
		CostCenterID: strPtr(types.StringValue("budget_42")),
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"costCenterId":"budget_42"`) {
		t.Errorf("create body must carry costCenterId, got %s", raw)
	}
}

// When no cost center is configured, costCenterId must be omitted from the body.
func TestAPIKeyCreateBodyOmitsCostCenterIDWhenNull(t *testing.T) {
	body := apiKeyCreateBody{
		Name:         "ci",
		CostCenterID: strPtr(types.StringNull()),
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), "costCenterId") {
		t.Errorf("create body must omit costCenterId when null, got %s", raw)
	}
}

// The update body must obey the same omitempty rules.
func TestAPIKeyUpdateBodyCostCenterID(t *testing.T) {
	with := apiKeyUpdateBody{CostCenterID: strPtr(types.StringValue("budget_7"))}
	raw, _ := json.Marshal(with)
	if !strings.Contains(string(raw), `"costCenterId":"budget_7"`) {
		t.Errorf("update body must carry costCenterId, got %s", raw)
	}
	without := apiKeyUpdateBody{CostCenterID: strPtr(types.StringNull())}
	raw, _ = json.Marshal(without)
	if strings.Contains(string(raw), "costCenterId") {
		t.Errorf("update body must omit costCenterId when null, got %s", raw)
	}
}

// strPtr maps null/unknown to nil and a set value to a pointer.
func TestStrPtr(t *testing.T) {
	if strPtr(types.StringNull()) != nil {
		t.Error("null => nil")
	}
	if strPtr(types.StringUnknown()) != nil {
		t.Error("unknown => nil")
	}
	if p := strPtr(types.StringValue("x")); p == nil || *p != "x" {
		t.Error("value => pointer to value")
	}
}

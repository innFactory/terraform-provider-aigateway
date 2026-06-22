package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// strListVal builds a types.List of strings for tests (ignoring diags).
func strListVal(in []string) types.List {
	v, _ := types.ListValueFrom(context.Background(), types.StringType, in)
	return v
}

func TestCompanygptIntegrationBodyMarshalsCamelCaseAndOmitsUnset(t *testing.T) {
	ctx := context.Background()
	m := companygptIntegrationResourceModel{
		TenantID:              types.StringValue("default"),
		Enabled:               types.BoolValue(true),
		ExternalTenantIDs:     strListVal([]string{"lc-tenant-1"}),
		DefaultUserStatus:     types.StringValue("active"),
		AllowUnbudgetedUsers:  types.BoolNull(),
		DefaultSharedBudgetID: types.StringNull(),
		RoleMappings: []roleMappingModel{{
			ExternalRole:             types.StringValue("user"),
			GatewayRole:              types.StringValue("member"),
			AllowedModels:            strListVal([]string{"gpt-5.4"}),
			UserBudgetMicrodollars:   types.Int64Value(5000000),
			SharedBudgetID:           types.StringNull(),
			PerUserLimitMicrodollars: types.Int64Null(),
			AllowUnbudgeted:          types.BoolNull(),
		}},
		GroupMappings: []groupMappingModel{{
			ExternalGroupID: types.StringValue("grp-admins"),
			GatewayRole:     types.StringValue("admin"),
			TeamID:          types.StringValue("team_1"),
		}},
		ManagedBy:       types.StringNull(), // → default
		ManagedRevision: types.StringNull(),
	}

	raw, err := json.Marshal(m.toBody(ctx))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"enabled":true,"externalTenantIds":["lc-tenant-1"],"defaultUserStatus":"active","roleMappings":[{"externalRole":"user","gatewayRole":"member","allowedModels":["gpt-5.4"],"userBudgetMicrodollars":5000000}],"groupMappings":[{"externalGroupId":"grp-admins","gatewayRole":"admin","teamId":"team_1"}],"metadata":{"managedBy":"companygpt-terraform"}}`
	if string(raw) != want {
		t.Errorf("toBody()=%s\nwant %s", raw, want)
	}
}

func TestCompanygptIntegrationGroupMappingAllowedProviders(t *testing.T) {
	ctx := context.Background()
	m := companygptIntegrationResourceModel{
		TenantID: types.StringValue("default"),
		Enabled:  types.BoolValue(true),
		GroupMappings: []groupMappingModel{{
			ExternalGroupID:  types.StringValue("grp-no-anthropic"),
			AllowedModels:    strListVal([]string{"gpt-5.4"}),
			AllowedProviders: strListVal([]string{"provider_openai"}),
		}},
		ManagedBy:       types.StringNull(),
		ManagedRevision: types.StringNull(),
	}
	raw, err := json.Marshal(m.toBody(ctx))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"enabled":true,"groupMappings":[{"externalGroupId":"grp-no-anthropic","allowedModels":["gpt-5.4"],"allowedProviders":["provider_openai"]}],"metadata":{"managedBy":"companygpt-terraform"}}`
	if string(raw) != want {
		t.Errorf("toBody()=%s\nwant %s", raw, want)
	}
}

func TestCompanygptIntegrationDisabledBody(t *testing.T) {
	body := upsertIntegrationPolicyBody{
		Enabled:  false,
		Metadata: &integrationMetadataBody{ManagedBy: defaultManagedBy},
	}
	raw, _ := json.Marshal(body)
	want := `{"enabled":false,"metadata":{"managedBy":"companygpt-terraform"}}`
	if string(raw) != want {
		t.Errorf("disabled body = %s, want %s", raw, want)
	}
}

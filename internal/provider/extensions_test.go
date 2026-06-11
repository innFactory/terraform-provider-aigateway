package provider

import (
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestDeploymentGroupBodyOmitsUnsetOptionals(t *testing.T) {
	m := deploymentGroupResourceModel{
		ModelID:  types.StringValue("gpt-5.4"),
		Strategy: types.StringValue("weighted_random"),
		Deployments: []deploymentModel{{
			ProviderID:      types.StringValue("provider_a"),
			ProviderModelID: types.StringValue("gpt-5.4"),
			DeploymentName:  types.StringValue("gpt-5.4"),
			Weight:          types.Int64Value(70),
			Priority:        types.Int64Null(),
			Enabled:         types.BoolValue(true),
			TimeoutSeconds:  types.Int64Null(),
		}},
	}
	raw, _ := json.Marshal(m.toBody())
	want := `{"deployments":[{"providerId":"provider_a","providerModelId":"gpt-5.4","deploymentName":"gpt-5.4","weight":70,"enabled":true}],"strategy":"weighted_random"}`
	if string(raw) != want {
		t.Errorf("toBody()=%s\nwant %s", raw, want)
	}
}

func TestFallbackChainBodyShape(t *testing.T) {
	b := fallbackChainBody{FallbackModels: []string{"gpt-5.4", "gpt-5.4-mini"}}
	raw, _ := json.Marshal(b)
	want := `{"fallbackModels":["gpt-5.4","gpt-5.4-mini"]}`
	if string(raw) != want {
		t.Errorf("got %s want %s", raw, want)
	}
}

package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// api_version is Optional+Computed: when it is unset in config (every non-Azure
// provider, e.g. Vertex) its planned value is UNKNOWN. apply() MUST resolve it
// to a known value, otherwise Terraform rejects the result with
// "Provider returned invalid result object after apply ... still indicated an
// unknown value for ...api_version". This is the bug that broke the 4 Vertex
// providers on the innfactory26 rollout.
func TestProviderApplyResolvesUnknownAPIVersion(t *testing.T) {
	r := &providerResource{}
	m := &providerResourceModel{
		APIVersion: types.StringUnknown(), // Vertex: unset in config => unknown plan value
		Region:     types.StringValue("eu"),
		ProjectID:  types.StringValue("proj-123"),
	}
	// The gateway omits apiVersion for non-Azure providers, so the decoded
	// response carries the empty string.
	a := &providerAPI{
		ID:         "provider_abc",
		Type:       "gemini",
		Name:       "Vertex AI Gemini",
		Endpoint:   "https://aiplatform.eu.rep.googleapis.com",
		AuthType:   "serviceAccount",
		Region:     "eu",
		ProjectID:  "proj-123",
		APIVersion: "",
		Enabled:    true,
	}

	r.apply(m, a)

	if m.APIVersion.IsUnknown() {
		t.Fatal("api_version is Computed and must be KNOWN after apply, got unknown")
	}
	if !m.APIVersion.IsNull() {
		t.Errorf("api_version should be null when the gateway returns none, got %q", m.APIVersion.ValueString())
	}
	if m.ID.ValueString() != "provider_abc" {
		t.Errorf("id mismatch: got %q", m.ID.ValueString())
	}
}

// Azure sets api_version in config; the gateway echoes it back. apply() must
// preserve the value exactly (Optional+Computed round-trip).
func TestProviderApplyPreservesAzureAPIVersion(t *testing.T) {
	r := &providerResource{}
	m := &providerResourceModel{APIVersion: types.StringValue("2024-10-21")}
	a := &providerAPI{ID: "provider_x", APIVersion: "2024-10-21", Enabled: true}

	r.apply(m, a)

	if m.APIVersion.ValueString() != "2024-10-21" {
		t.Errorf("azure api_version must round-trip, got %q", m.APIVersion.ValueString())
	}
}

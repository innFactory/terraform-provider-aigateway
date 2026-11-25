package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func strListOf(vals ...string) types.List {
	elems := make([]attr.Value, len(vals))
	for i, v := range vals {
		elems[i] = types.StringValue(v)
	}
	return types.ListValueMust(types.StringType, elems)
}

func ccTestSchema(t *testing.T) resource.SchemaResponse {
	t.Helper()
	var sr resource.SchemaResponse
	(&costCenterResource{}).Schema(context.Background(), resource.SchemaRequest{}, &sr)
	if sr.Diagnostics.HasError() {
		t.Fatalf("schema: %v", sr.Diagnostics)
	}
	return sr
}

func ccState(t *testing.T, m costCenterResourceModel) tfsdk.State {
	t.Helper()
	st := tfsdk.State{Schema: ccTestSchema(t).Schema}
	if d := st.Set(context.Background(), m); d.HasError() {
		t.Fatalf("state set: %v", d)
	}
	return st
}

func ccPlan(t *testing.T, m costCenterResourceModel) tfsdk.Plan {
	t.Helper()
	p := tfsdk.Plan{Schema: ccTestSchema(t).Schema}
	if d := p.Set(context.Background(), m); d.HasError() {
		t.Fatalf("plan set: %v", d)
	}
	return p
}

func ccTestServer(t *testing.T, handler http.HandlerFunc) *costCenterResource {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &costCenterResource{client: newClient(srv.URL, "test-key", "test")}
}

func ccBaseModel(access *modelAccessModel) costCenterResourceModel {
	return costCenterResourceModel{
		ID: types.StringValue("budget_1"), Name: types.StringValue("cc"), Currency: types.StringValue("EUR"), FallbackChain: types.ListNull(types.StringType),
		ModelAccess: access,
	}
}

// model_access set -> the POST and PATCH bodies carry modelAccess with both
// lists; absent -> no modelAccess key (Terraform does not manage it).
func TestCostCenterModelAccessOnTheWire(t *testing.T) {
	both := &modelAccessModel{AllowedModels: strListOf("gpt-5.4"), AllowedProviders: strListOf("azure-eu")}
	onlyProviders := &modelAccessModel{AllowedModels: types.ListNull(types.StringType), AllowedProviders: strListOf("azure-eu")}
	tests := []struct {
		name   string
		method string
		access *modelAccessModel
		want   any // nil = key absent
	}{
		{"create sends both lists", "POST", both, map[string]any{"allowedModels": []any{"gpt-5.4"}, "allowedProviders": []any{"azure-eu"}}},
		{"create with one list sends an empty array for the other", "POST", onlyProviders, map[string]any{"allowedModels": []any{}, "allowedProviders": []any{"azure-eu"}}},
		{"create omits it when absent", "POST", nil, nil},
		{"update sends both lists", "PATCH", both, map[string]any{"allowedModels": []any{"gpt-5.4"}, "allowedProviders": []any{"azure-eu"}}},
		{"update omits it when absent", "PATCH", nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got map[string]any
			r := ccTestServer(t, func(w http.ResponseWriter, req *http.Request) {
				if req.Method == tc.method {
					got = decodeBody(t, req)
				}
				out := costCenterAPI{ID: "budget_1", Name: "cc", Currency: "EUR"}
				if tc.access != nil {
					out.ModelAccess = &modelAccessAPI{AllowedModels: listOrNil(context.Background(), tc.access.AllowedModels), AllowedProviders: listOrNil(context.Background(), tc.access.AllowedProviders)}
				}
				_ = json.NewEncoder(w).Encode(out)
			})
			m := ccBaseModel(tc.access)
			var errs bool
			if tc.method == "POST" {
				resp := &resource.CreateResponse{State: ccState(t, m)}
				r.Create(context.Background(), resource.CreateRequest{Plan: ccPlan(t, m)}, resp)
				errs = resp.Diagnostics.HasError()
				if errs {
					t.Fatalf("diagnostics: %v", resp.Diagnostics)
				}
			} else {
				resp := &resource.UpdateResponse{State: ccState(t, m)}
				r.Update(context.Background(), resource.UpdateRequest{Plan: ccPlan(t, m), State: ccState(t, m)}, resp)
				if resp.Diagnostics.HasError() {
					t.Fatalf("diagnostics: %v", resp.Diagnostics)
				}
			}
			if got == nil {
				t.Fatalf("no %s captured", tc.method)
			}
			val, present := got["modelAccess"]
			if tc.want == nil {
				if present {
					t.Fatalf("modelAccess must be absent, got %v", val)
				}
				return
			}
			gotJSON, _ := json.Marshal(val)
			wantJSON, _ := json.Marshal(tc.want)
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("modelAccess = %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

// Read with a server modelAccess while the state has none -> stays null (not
// managed); while managed, the server value is reflected (drift shows).
func TestCostCenterApplyModelAccessOnlyWhenManaged(t *testing.T) {
	r := &costCenterResource{}
	server := &costCenterAPI{ID: "b", Name: "cc", Currency: "EUR", ModelAccess: &modelAccessAPI{AllowedModels: []string{"gpt-5.4"}}}

	unmanaged := &costCenterResourceModel{}
	r.apply(unmanaged, server, "EUR")
	if unmanaged.ModelAccess != nil {
		t.Errorf("an unmanaged model_access must stay null, got %+v", unmanaged.ModelAccess)
	}

	managed := &costCenterResourceModel{ModelAccess: &modelAccessModel{AllowedModels: strListOf("old"), AllowedProviders: types.ListNull(types.StringType)}}
	r.apply(managed, server, "EUR")
	if managed.ModelAccess == nil || !managed.ModelAccess.AllowedModels.Equal(strListOf("gpt-5.4")) {
		t.Fatalf("a managed model_access must follow the server, got %+v", managed.ModelAccess)
	}
	if !managed.ModelAccess.AllowedProviders.IsNull() {
		t.Errorf("an empty server list for a list the config left null must stay null, got %v", managed.ModelAccess.AllowedProviders)
	}

	cleared := &costCenterResourceModel{ModelAccess: &modelAccessModel{AllowedModels: strListOf("old"), AllowedProviders: types.ListNull(types.StringType)}}
	r.apply(cleared, &costCenterAPI{ID: "b", Name: "cc", Currency: "EUR"}, "EUR")
	if cleared.ModelAccess != nil {
		t.Errorf("a model_access cleared on the gateway must read as null (drift), got %+v", cleared.ModelAccess)
	}
}

// Plan-time validation mirrors the gateway's 400: nothing listed (after
// trimming) is refused so `terraform plan` fails before any apply.
func TestModelAccessValidate(t *testing.T) {
	ctx := context.Background()
	unknown := types.ListUnknown(types.StringType)
	tests := []struct {
		name    string
		m       modelAccessModel
		wantErr bool
	}{
		{"models listed", modelAccessModel{AllowedModels: strListOf("a"), AllowedProviders: types.ListNull(types.StringType)}, false},
		{"providers listed", modelAccessModel{AllowedModels: types.ListNull(types.StringType), AllowedProviders: strListOf("p")}, false},
		{"both null", modelAccessModel{AllowedModels: types.ListNull(types.StringType), AllowedProviders: types.ListNull(types.StringType)}, true},
		{"both empty", modelAccessModel{AllowedModels: strListOf(), AllowedProviders: strListOf()}, true},
		{"blank entries only", modelAccessModel{AllowedModels: strListOf("  ", ""), AllowedProviders: strListOf(" ")}, true},
		{"unknown list defers", modelAccessModel{AllowedModels: unknown, AllowedProviders: types.ListNull(types.StringType)}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := validateModelAccess(ctx, &tc.m)
			if (msg != "") != tc.wantErr {
				t.Errorf("validateModelAccess = %q, wantErr %v", msg, tc.wantErr)
			}
			if tc.wantErr && !strings.Contains(msg, "remove model_access to allow every model") {
				t.Errorf("message must tell the fix, got %q", msg)
			}
		})
	}
}

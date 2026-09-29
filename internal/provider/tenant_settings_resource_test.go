package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// managed_revision must be Computed-only with NO plan modifiers. write() stamps
// a fresh time.Now() on every apply, so the value changes on each update. The
// previous schema used Optional+Computed+UseStateForUnknown, which pinned the
// prior timestamp in the plan while the apply returned a new one -> "Provider
// produced inconsistent result after apply" on every tenant_settings change.
// This guards against re-introducing that bug.
func TestTenantSettingsManagedRevisionIsComputedOnly(t *testing.T) {
	r := &tenantSettingsResource{}
	var resp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &resp)

	raw, ok := resp.Schema.Attributes["managed_revision"]
	if !ok {
		t.Fatal("managed_revision attribute missing from schema")
	}
	attr, ok := raw.(schema.StringAttribute)
	if !ok {
		t.Fatalf("managed_revision is %T, want schema.StringAttribute", raw)
	}
	if !attr.Computed {
		t.Error("managed_revision must be Computed (provider-stamped)")
	}
	if attr.Optional {
		t.Error("managed_revision must NOT be Optional — it is provider-managed, not user-set")
	}
	if len(attr.PlanModifiers) != 0 {
		t.Errorf("managed_revision must have NO plan modifiers (UseStateForUnknown caused inconsistent-result-after-apply); got %d", len(attr.PlanModifiers))
	}
}

// A fully-managed PATCH body: org cap 0 (unlimited sentinel), a set per-user cap
// (microdollar value), currency, default cost center, revision.
func TestTenantPatchBodyMarshalsFull(t *testing.T) {
	body := tenantPatchBody{
		DefaultAllowedModels:    []string{"gpt-5.4"},
		OrgBudgetMicros:         ptrInt64(0),
		Currency:                "EUR",
		DefaultUserBudgetMicros: json.RawMessage("50000000"),
		DefaultCostCenterID:     json.RawMessage(`"budget_companygpt"`),
		DefaultAccessGroupID:    "team_default",
		ManagedRevision:         "2026-06-21T10:00:00Z",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	want := `{"defaultAllowedModels":["gpt-5.4"],"orgBudgetLimitMicrodollars":0,"currency":"EUR","defaultUserBudgetMicrodollars":50000000,"defaultCostCenterId":"budget_companygpt","defaultAccessGroupId":"team_default","managedRevision":"2026-06-21T10:00:00Z"}`
	if got != want {
		t.Errorf("patch body mismatch\n got: %s\nwant: %s", got, want)
	}
}

// default_user_budget_unlimited=true emits "defaultUserBudgetMicrodollars":null
// (not 0). The gateway's double-option field treats null as "clear the cap";
// sending 0 would BLOCK all users (a zero-dollar per-user cap).
func TestTenantPatchBodyUserUnlimitedSerialisesNull(t *testing.T) {
	body := tenantPatchBody{
		DefaultAllowedModels:    []string{"gpt-4o"},
		OrgBudgetMicros:         ptrInt64(0),
		Currency:                "USD",
		DefaultUserBudgetMicros: json.RawMessage("null"), // unlimited: explicit null clears
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

// NEW (v0.9.2 config-driven budgets): when the tenant leaves org/user budget
// UNSET (nil), both keys are OMITTED from the PATCH — the gateway then leaves
// its (config.yaml-driven) values untouched instead of clearing them. This is
// what lets budgets live entirely in gateway-config.yaml.
func TestTenantPatchBodyOmitsUnsetBudgets(t *testing.T) {
	body := tenantPatchBody{
		DefaultAllowedModels:    []string{"gpt-4o"},
		OrgBudgetMicros:         nil, // unset → omitted
		DefaultUserBudgetMicros: nil, // unset → omitted
		Currency:                "EUR",
		ManagedRevision:         "2026-06-21T10:00:00Z",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	if strings.Contains(got, "orgBudgetLimitMicrodollars") {
		t.Errorf("unset org budget must be omitted, got: %s", got)
	}
	if strings.Contains(got, "defaultUserBudgetMicrodollars") {
		t.Errorf("unset user budget must be omitted, got: %s", got)
	}
}

// Read must NOT overwrite the configured mutable fields from the gateway
// response (last-writer-wins / config-driven: a dashboard or config.yaml edit
// must not be reverted). applyTenantRead is now a no-op over these fields.
func TestTenantSettingsReadDoesNotRevertMutableFields(t *testing.T) {
	state := tenantSettingsResourceModel{
		Currency:                types.StringValue("EUR"),
		DefaultUserBudgetMicros: types.Int64Value(50000000),
		DefaultCostCenterID:     types.StringValue("budget_companygpt"),
		DefaultAccessGroupID:    types.StringValue("team_default"),
		OrgBudgetMicros:         types.Int64Value(500000000),
	}
	// A gateway GET reporting DIFFERENT values (a dashboard/config edit). Read
	// must leave the planned/state values untouched.
	out := tenantAPI{
		Currency:                      "USD",
		DefaultUserBudgetMicrodollars: ptrInt64(999),
		DefaultCostCenterID:           "budget_other",
		DefaultAccessGroupID:          "team_other",
		OrgBudget: &struct {
			MonthlyLimitMicrodollars *int64 `json:"monthlyLimitMicrodollars"`
		}{MonthlyLimitMicrodollars: ptrInt64(1)},
	}
	applyTenantRead(&state, &out)
	if state.Currency.ValueString() != "EUR" {
		t.Errorf("currency reverted to %q, want EUR", state.Currency.ValueString())
	}
	if state.DefaultUserBudgetMicros.ValueInt64() != 50000000 {
		t.Errorf("user max reverted to %d", state.DefaultUserBudgetMicros.ValueInt64())
	}
	if state.OrgBudgetMicros.ValueInt64() != 500000000 {
		t.Errorf("org budget reverted to %d (config-driven must not drift)", state.OrgBudgetMicros.ValueInt64())
	}
	if state.DefaultCostCenterID.ValueString() != "budget_companygpt" {
		t.Errorf("default cost center reverted to %q", state.DefaultCostCenterID.ValueString())
	}
	if state.DefaultAccessGroupID.ValueString() != "team_default" {
		t.Errorf("default access group reverted to %q", state.DefaultAccessGroupID.ValueString())
	}
}

// An unset default_access_group_id must be OMITTED from the PATCH body
// (omitempty): the gateway treats an absent key as "leave unchanged".
func TestTenantPatchBodyOmitsUnsetDefaultAccessGroup(t *testing.T) {
	body := tenantPatchBody{
		DefaultAllowedModels:    []string{"gpt-4o"},
		OrgBudgetMicros:         ptrInt64(0),
		DefaultUserBudgetMicros: json.RawMessage("null"),
		ManagedRevision:         "2026-06-21T10:00:00Z",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	if strings.Contains(got, "defaultAccessGroupId") {
		t.Errorf("unset default access group must be omitted, got: %s", got)
	}
}

// Cost-margin knobs set to an explicit 0 MUST appear in the PATCH body (a 0
// margin is meaningful: it makes customer_cost == provider_cost).
func TestTenantPatchBodyMarginZeroIsSent(t *testing.T) {
	pct := float64(0)
	margin := int64(0)
	body := tenantPatchBody{
		DefaultAllowedModels:   []string{"gpt-4o"},
		OrgBudgetMicros:        ptrInt64(0),
		AzureCommissionPercent: &pct,
		ExternalMarginMicros:   &margin,
		ManagedRevision:        "2026-06-21T10:00:00Z",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, `"azureCommissionPercent":0`) ||
		!strings.Contains(got, `"externalMarginPer1mTokensMicrodollars":0`) {
		t.Errorf("explicit 0 margins must be sent, got: %s", got)
	}
}

// Unset cost-margin knobs (nil pointers) MUST be omitted so the gateway keeps
// its default / a dashboard edit is not reverted (last-writer-wins).
func TestTenantPatchBodyOmitsUnsetMargins(t *testing.T) {
	body := tenantPatchBody{
		DefaultAllowedModels: []string{"gpt-4o"},
		OrgBudgetMicros:      ptrInt64(0),
		ManagedRevision:      "2026-06-21T10:00:00Z",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	if strings.Contains(got, "azureCommissionPercent") || strings.Contains(got, "externalMarginPer1mTokensMicrodollars") {
		t.Errorf("unset margins must be omitted, got: %s", got)
	}
}

// default_cost_center_id tri-state: set sends the id, removing it after it was
// set sends an explicit null (the gateway clears on null), and unset before and
// after omits the key so a dashboard-set value is not reverted.
func TestDefaultCostCenterPatch(t *testing.T) {
	set := types.StringValue("budget_companygpt")
	null := types.StringNull()
	empty := types.StringValue("")
	cases := []struct {
		name  string
		plan  types.String
		prior *types.String
		want  string // "" = omitted (nil)
	}{
		{"create with id", set, nil, `"budget_companygpt"`},
		{"create unset", null, nil, ""},
		{"update keeps id", set, &set, `"budget_companygpt"`},
		{"update changes id", types.StringValue("budget_org"), &set, `"budget_org"`},
		{"update removed after set clears", null, &set, `null`},
		{"update emptied after set clears", empty, &set, `null`},
		{"update unset before and after omits", null, &null, ""},
		{"update set from unset", set, &null, `"budget_companygpt"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := defaultCostCenterPatch(c.plan, c.prior)
			if c.want == "" {
				if got != nil {
					t.Fatalf("want omitted (nil), got %s", got)
				}
				return
			}
			if string(got) != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}

// End to end over the wire: an Update whose prior state held a default cost
// center and whose plan no longer does must PATCH "defaultCostCenterId":null.
// The next Update (unset before and after) must not send the key at all.
func TestTenantSettingsWriteClearsRemovedDefaultCostCenter(t *testing.T) {
	var bodies []map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/admin/tenant" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var b map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			t.Errorf("decode body: %v", err)
		}
		bodies = append(bodies, b)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	r := &tenantSettingsResource{client: newClient(srv.URL, "k", "test")}
	var errs []string
	sink := &diagSink{add: func(s, d string) { errs = append(errs, s+": "+d) }}

	prior := tenantSettingsResourceModel{
		DefaultAllowedModels: types.ListNull(types.StringType),
		DefaultCostCenterID:  types.StringValue("budget_companygpt"),
	}
	plan := tenantSettingsResourceModel{
		DefaultAllowedModels: types.ListNull(types.StringType),
		DefaultCostCenterID:  types.StringNull(),
	}
	r.write(context.Background(), &plan, &prior, sink)
	// Next apply: the persisted state now holds null, the config is still unset.
	r.write(context.Background(), &plan, &plan, sink)

	if len(errs) != 0 {
		t.Fatalf("write errors: %v", errs)
	}
	if len(bodies) != 2 {
		t.Fatalf("want 2 PATCH calls, got %d", len(bodies))
	}
	if got, ok := bodies[0]["defaultCostCenterId"]; !ok || string(got) != "null" {
		t.Errorf("first PATCH must clear with null, got %q (present=%v)", got, ok)
	}
	if got, ok := bodies[1]["defaultCostCenterId"]; ok {
		t.Errorf("second PATCH must omit defaultCostCenterId, got %s", got)
	}
}

func ptrInt64(v int64) *int64 { return &v }

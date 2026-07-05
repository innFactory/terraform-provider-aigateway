package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/defaults"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// The create body must marshal to the gateway's camelCase CreateTeamRequest
// shape (src/routes/admin/teams.rs), omitting unset optionals.
func TestAccessGroupCreateBodyMarshalsFull(t *testing.T) {
	desc := "Engineering access"
	icon := "🚀"
	entra := "00000000-0000-0000-0000-000000000001"
	budget := int64(5_000_000)
	rpm := int64(60)
	body := accessGroupCreateBody{
		Name:                    "engineering",
		Description:             &desc,
		Icon:                    &icon,
		BudgetLimitMicrodollars: &budget,
		RateLimitRpm:            &rpm,
		AllowedModels:           []string{"gpt-5.4"},
		AllowedProviders:        []string{"provider_azure"},
		EntraGroupID:            &entra,
		SupportedFormats:        []string{"openai", "anthropic"},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	want := `{"name":"engineering","description":"Engineering access","icon":"🚀","budgetLimitMicrodollars":5000000,"rateLimitRpm":60,"allowedModels":["gpt-5.4"],"allowedProviders":["provider_azure"],"entraGroupId":"00000000-0000-0000-0000-000000000001","supportedFormats":["openai","anthropic"]}`
	if got != want {
		t.Errorf("create body mismatch\n got: %s\nwant: %s", got, want)
	}
}

// With only the required name set, all optional fields must be omitted —
// the gateway create endpoint treats absent as None (no restriction).
func TestAccessGroupCreateBodyOmitsOptional(t *testing.T) {
	body := accessGroupCreateBody{Name: "everyone"}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	want := `{"name":"everyone"}`
	if got != want {
		t.Errorf("create body mismatch\n got: %s\nwant: %s", got, want)
	}
}

// The update body always transmits supportedFormats (empty array clears the
// restriction server-side: empty vec → None → all formats) and enabled (the
// planned value is always known via the schema default). The non-clearable
// fields keep omitempty so an absent field means "leave unchanged".
func TestAccessGroupUpdateBodySendsFormatsAndEnabled(t *testing.T) {
	enabled := true
	body := accessGroupUpdateBody{
		Name:             strp("engineering"),
		SupportedFormats: []string{},
		Enabled:          &enabled,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	want := `{"name":"engineering","supportedFormats":[],"enabled":true}`
	if got != want {
		t.Errorf("update body mismatch\n got: %s\nwant: %s", got, want)
	}
}

// enabled must be Optional+Computed WITH a static default(true) — never bare
// Optional+Computed. Bare Optional+Computed plans "unknown" on update and
// (without UseStateForUnknown) sends false to the gateway, silently disabling
// the resource. The static default keeps the planned value known (true).
func TestAccessGroupEnabledHasStaticDefault(t *testing.T) {
	r := &accessGroupResource{}
	var resp resource.SchemaResponse
	r.Schema(context.Background(), resource.SchemaRequest{}, &resp)

	raw, ok := resp.Schema.Attributes["enabled"]
	if !ok {
		t.Fatal("enabled attribute missing from schema")
	}
	attr, ok := raw.(schema.BoolAttribute)
	if !ok {
		t.Fatalf("enabled is %T, want schema.BoolAttribute", raw)
	}
	if !attr.Optional || !attr.Computed {
		t.Error("enabled must be Optional+Computed (required for a schema default)")
	}
	if attr.Default == nil {
		t.Fatal("enabled must carry a static default(true) — bare Optional+Computed sends false on update")
	}
	if got := staticBoolDefault(t, attr); got != true {
		t.Errorf("enabled default = %v, want true", got)
	}
}

// staticBoolDefault resolves the schema default the way the framework does.
func staticBoolDefault(t *testing.T, attr schema.BoolAttribute) bool {
	t.Helper()
	var resp defaults.BoolResponse
	attr.Default.DefaultBool(context.Background(), defaults.BoolRequest{}, &resp)
	if resp.PlanValue.IsNull() || resp.PlanValue.IsUnknown() {
		t.Fatal("enabled default did not produce a known value")
	}
	return resp.PlanValue.ValueBool()
}

// ValidateConfig must reject formats outside the gateway ApiFormat enum
// (openai | anthropic | gemini, lowercase — src/models/team.rs).
func TestAccessGroupValidateConfigRejectsBadFormat(t *testing.T) {
	cases := []struct {
		formats []string
		wantErr bool
	}{
		{nil, false},
		{[]string{"openai"}, false},
		{[]string{"openai", "anthropic", "gemini"}, false},
		{[]string{"OpenAI"}, true}, // wire tokens are lowercase
		{[]string{"grpc"}, true},
	}
	for _, c := range cases {
		diags := validateFormats(c.formats)
		if c.wantErr && !diags {
			t.Errorf("formats %v: expected a validation error", c.formats)
		}
		if !c.wantErr && diags {
			t.Errorf("formats %v: unexpected validation error", c.formats)
		}
	}
}

// validateFormats runs the same membership check ValidateConfig performs and
// reports whether any element is invalid.
func validateFormats(formats []string) bool {
	for _, f := range formats {
		valid := false
		for _, v := range validAPIFormats {
			if f == v {
				valid = true
				break
			}
		}
		if !valid {
			return true
		}
	}
	return false
}

// apply(refresh=false) must round-trip known planned values from the gateway
// echo and reflect the enabled flag.
func TestAccessGroupApplyRoundTripsKnownValues(t *testing.T) {
	r := &accessGroupResource{}
	desc := "Engineering access"
	budget := int64(5_000_000)
	m := &accessGroupResourceModel{
		Name:                    types.StringValue("engineering"),
		Description:             types.StringValue("Engineering access"),
		BudgetLimitMicrodollars: types.Int64Value(5_000_000),
		Enabled:                 types.BoolValue(true),
	}
	a := &accessGroupAPI{
		ID:                      "team-1",
		Name:                    "engineering",
		Description:             &desc,
		BudgetLimitMicrodollars: &budget,
		Enabled:                 true,
	}
	r.apply(m, a, false)
	if m.ID.ValueString() != "team-1" {
		t.Errorf("id = %q, want team-1", m.ID.ValueString())
	}
	if m.Description.ValueString() != "Engineering access" {
		t.Errorf("description must round-trip, got %q", m.Description.ValueString())
	}
	if m.BudgetLimitMicrodollars.ValueInt64() != 5_000_000 {
		t.Errorf("budget must round-trip, got %d", m.BudgetLimitMicrodollars.ValueInt64())
	}
	if !m.Enabled.ValueBool() {
		t.Error("enabled must reflect the gateway flag")
	}
}

// apply(refresh=false) must NOT overwrite a planned null with a leftover
// server value: the teams PATCH cannot clear these fields, and reflecting the
// leftover would be "Provider produced inconsistent result after apply".
func TestAccessGroupApplyKeepsPlannedNullOnUpdate(t *testing.T) {
	r := &accessGroupResource{}
	desc := "old description"
	rpm := int64(60)
	m := &accessGroupResourceModel{
		Name:         types.StringValue("engineering"),
		Description:  types.StringNull(), // removed from config
		RateLimitRpm: types.Int64Null(),  // removed from config
		Enabled:      types.BoolValue(true),
	}
	a := &accessGroupAPI{
		ID:           "team-1",
		Name:         "engineering",
		Description:  &desc, // gateway still holds the old value
		RateLimitRpm: &rpm,
		Enabled:      true,
	}
	r.apply(m, a, false)
	if !m.Description.IsNull() {
		t.Errorf("description should stay null when unset in plan, got %q", m.Description.ValueString())
	}
	if !m.RateLimitRpm.IsNull() {
		t.Errorf("rate_limit_rpm should stay null when unset in plan, got %d", m.RateLimitRpm.ValueInt64())
	}
}

// apply(refresh=true) must reflect the server unconditionally — including a
// null when the server no longer carries a field — so out-of-band changes
// surface as drift in `terraform plan`.
func TestAccessGroupApplyRefreshReflectsServer(t *testing.T) {
	r := &accessGroupResource{}
	icon := "🔒"
	m := &accessGroupResourceModel{
		Name:        types.StringValue("engineering"),
		Description: types.StringValue("stale state value"),
		Icon:        types.StringNull(),
		Enabled:     types.BoolValue(true),
	}
	a := &accessGroupAPI{
		ID:      "team-1",
		Name:    "engineering-renamed",
		Icon:    &icon,
		Enabled: false, // disabled out-of-band
	}
	r.apply(m, a, true)
	if m.Name.ValueString() != "engineering-renamed" {
		t.Errorf("refresh must reflect the server name, got %q", m.Name.ValueString())
	}
	if !m.Description.IsNull() {
		t.Errorf("refresh must null a field the server no longer carries, got %q", m.Description.ValueString())
	}
	if m.Icon.ValueString() != "🔒" {
		t.Errorf("refresh must reflect the server icon, got %q", m.Icon.ValueString())
	}
	if m.Enabled.ValueBool() {
		t.Error("refresh must reflect enabled=false (out-of-band disable → drift)")
	}
}

// The gateway TeamResponse (camelCase) must decode into accessGroupAPI.
func TestAccessGroupAPIDecodesTeamResponse(t *testing.T) {
	raw := `{
		"id": "t1",
		"tenantId": "default",
		"name": "Engineering",
		"description": "desc",
		"icon": "🚀",
		"budgetLimitMicrodollars": 5000000,
		"rateLimitRpm": 60,
		"allowedModels": ["gpt-5.4"],
		"allowedProviders": ["provider_azure"],
		"entraGroupId": "grp-1",
		"supportedFormats": ["openai"],
		"enabled": true,
		"createdAt": "2026-07-01T00:00:00Z",
		"updatedAt": "2026-07-01T00:00:00Z"
	}`
	var a accessGroupAPI
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if a.ID != "t1" || a.Name != "Engineering" {
		t.Errorf("id/name mismatch: %q %q", a.ID, a.Name)
	}
	if a.BudgetLimitMicrodollars == nil || *a.BudgetLimitMicrodollars != 5000000 {
		t.Error("budgetLimitMicrodollars must decode")
	}
	if a.RateLimitRpm == nil || *a.RateLimitRpm != 60 {
		t.Error("rateLimitRpm must decode")
	}
	if len(a.SupportedFormats) != 1 || a.SupportedFormats[0] != "openai" {
		t.Errorf("supportedFormats mismatch: %v", a.SupportedFormats)
	}
	if a.EntraGroupID == nil || *a.EntraGroupID != "grp-1" {
		t.Error("entraGroupId must decode")
	}
	if !a.Enabled {
		t.Error("enabled must decode")
	}
}

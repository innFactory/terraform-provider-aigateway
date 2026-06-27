package provider

// aigateway_guardrail manages a guardrail policy on the AI gateway.
//
// rules is modeled as a JSON-encoded string attribute (types.String) rather
// than nested blocks. GuardrailRule is a serde-tagged enum — the variants
// differ in which fields they carry (prompt_injection has no extra fields,
// denied_topics has a topics list, banned_keywords has keywords + regex_patterns,
// etc.). Encoding a tagged-union set as nested blocks would require either a
// separate block type per variant (verbose, not extensible) or a catch-all
// dynamic attribute (loses type-checking). A JSON string keeps the HCL
// concise, avoids provider-side schema churn when new rule types are added, and
// lets the caller write/copy the exact JSON they'd POST to the admin API.
// The provider json.Unmarshal's the string into []any before sending so it is
// transmitted as a real JSON array, not a quoted string, on the wire.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// parseRulesJSON is a guardrail-specific alias of the shared parseJSONArray,
// kept for backward compatibility with existing tests and call sites.
func parseRulesJSON(s string) ([]any, error) {
	return parseJSONArray(s)
}

var (
	_ resource.Resource                     = (*guardrailResource)(nil)
	_ resource.ResourceWithConfigure        = (*guardrailResource)(nil)
	_ resource.ResourceWithImportState      = (*guardrailResource)(nil)
	_ resource.ResourceWithConfigValidators = (*guardrailResource)(nil)
)

type guardrailResource struct {
	client *httpClient
}

func newGuardrailResource() resource.Resource {
	return &guardrailResource{}
}

type guardrailResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Enabled     types.Bool   `tfsdk:"enabled"`
	// rules is a JSON-encoded string representing []GuardrailRule.
	// See the package-level comment for the rationale.
	Rules types.String `tfsdk:"rules"`
}

func (r *guardrailResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_guardrail"
}

func (r *guardrailResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A guardrail policy on the AI gateway. " +
			"Policies are composed of typed rules (prompt_injection, denied_topics, " +
			"banned_keywords, secret_detection, etc.) that are evaluated against " +
			"requests and/or responses. Assign policies to scopes via a separate " +
			"aigateway_guardrail_assignment resource (when available).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Server-assigned policy id (uuid).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Unique name for this guardrail policy.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Optional human-readable description of the policy.",
			},
			"enabled": schema.BoolAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Whether the policy is active. Defaults to true.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			// rules is a JSON-encoded array of GuardrailRule objects.
			// Each element must be a tagged-enum object, e.g.:
			//   [{"type":"prompt_injection","action":"block"},
			//    {"type":"denied_topics","action":"warn","topics":["violence"]}]
			// The provider validates the JSON at plan time via the /validate endpoint
			// when the gateway is reachable, and always validates that the value is
			// well-formed JSON before sending.
			"rules": schema.StringAttribute{
				Required: true,
				Description: "JSON-encoded array of GuardrailRule objects. " +
					"Each rule is a tagged-enum object with a required \"type\" field " +
					"(prompt_injection | denied_topics | banned_keywords | secret_detection | " +
					"pii_detection | content_safety | rate_limit_guard) and an \"action\" field " +
					"(block | redact | warn | audit). Additional fields depend on the type. " +
					"Example: [{\"type\":\"prompt_injection\",\"action\":\"block\"}]",
			},
		},
	}
}

func (r *guardrailResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*httpClient)
}

// ConfigValidators calls the gateway /validate endpoint at plan time so that
// rule errors surface before apply. The check is best-effort: if the provider
// is not yet configured (nil client) we skip validation silently rather than
// failing the plan.
func (r *guardrailResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		&guardrailRulesValidator{client: &r.client},
	}
}

// guardrailRulesValidator is a resource.ConfigValidator that calls the gateway
// /validate endpoint. It holds a pointer-to-pointer so it always sees the
// client that Configure() stores on the resource after provider setup.
type guardrailRulesValidator struct {
	client **httpClient
}

func (v *guardrailRulesValidator) Description(_ context.Context) string {
	return "Validates guardrail rules against the gateway /validate endpoint."
}

func (v *guardrailRulesValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v *guardrailRulesValidator) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	// Skip if provider not configured yet.
	if v.client == nil || *v.client == nil {
		return
	}

	var m guardrailResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m.Rules.IsNull() || m.Rules.IsUnknown() {
		return
	}

	rules, err := parseRulesJSON(m.Rules.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("rules"), "Invalid rules JSON", err.Error())
		return
	}

	body := guardrailValidateRequest{Rules: rules}
	var out guardrailValidateResponse
	if err := (*v.client).do(ctx, "POST", "/api/v1/admin/guardrails/validate", nil, body, &out); err != nil {
		// Validation endpoint unreachable — skip (don't block the plan).
		return
	}
	if !out.Valid {
		for _, e := range out.Errors {
			resp.Diagnostics.AddAttributeError(path.Root("rules"), "Invalid guardrail rules", e)
		}
	}
}

// ---------------------------------------------------------------------------
// Wire types
// ---------------------------------------------------------------------------

type guardrailCreateBody struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Enabled     bool    `json:"enabled"`
	Rules       []any   `json:"rules"`
}

type guardrailUpdateBody struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Enabled     *bool   `json:"enabled,omitempty"`
	Rules       []any   `json:"rules,omitempty"`
}

type guardrailAPI struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description *string         `json:"description"`
	Enabled     bool            `json:"enabled"`
	Rules       json.RawMessage `json:"rules"`
}

type guardrailValidateRequest struct {
	Rules []any `json:"rules"`
}

type guardrailValidateResponse struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors"`
}

// ---------------------------------------------------------------------------
// CRUD
// ---------------------------------------------------------------------------

func (r *guardrailResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan guardrailResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	rules, err := parseRulesJSON(plan.Rules.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("rules"), "Invalid rules JSON", err.Error())
		return
	}

	body := guardrailCreateBody{
		Name:        plan.Name.ValueString(),
		Description: ptrIf(plan.Description),
		Enabled:     defBool(plan.Enabled, true),
		Rules:       rules,
	}

	var out guardrailAPI
	if err := r.client.do(ctx, "POST", "/api/v1/admin/guardrails/policies", nil, body, &out); err != nil {
		resp.Diagnostics.AddError("Create guardrail failed", err.Error())
		return
	}
	if err := r.apply(&plan, &out); err != nil {
		resp.Diagnostics.AddError("Apply guardrail response failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *guardrailResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state guardrailResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var out guardrailAPI
	err := r.client.do(ctx, "GET", "/api/v1/admin/guardrails/policies/"+state.ID.ValueString(), nil, nil, &out)
	if isNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Read guardrail failed", err.Error())
		return
	}
	if err := r.apply(&state, &out); err != nil {
		resp.Diagnostics.AddError("Apply guardrail response failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *guardrailResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan guardrailResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	var state guardrailResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	rules, err := parseRulesJSON(plan.Rules.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("rules"), "Invalid rules JSON", err.Error())
		return
	}

	name := plan.Name.ValueString()
	enabled := defBool(plan.Enabled, true)
	body := guardrailUpdateBody{
		Name:        &name,
		Description: ptrIf(plan.Description),
		Enabled:     &enabled,
		Rules:       rules,
	}

	var out guardrailAPI
	if err := r.client.do(ctx, "PATCH", "/api/v1/admin/guardrails/policies/"+state.ID.ValueString(), nil, body, &out); err != nil {
		resp.Diagnostics.AddError("Update guardrail failed", err.Error())
		return
	}
	plan.ID = state.ID
	if err := r.apply(&plan, &out); err != nil {
		resp.Diagnostics.AddError("Apply guardrail response failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *guardrailResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state guardrailResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.do(ctx, "DELETE", "/api/v1/admin/guardrails/policies/"+state.ID.ValueString(), nil, nil, nil); err != nil && !isNotFound(err) {
		resp.Diagnostics.AddError("Delete guardrail failed", err.Error())
	}
}

func (r *guardrailResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// ---------------------------------------------------------------------------
// apply: maps an API response back onto the model
// ---------------------------------------------------------------------------

// apply copies the server response fields into m. rules is round-tripped as
// a compact JSON string. description is Optional (not Computed): only reflected
// when the server returns a non-empty value, to avoid inconsistent-result errors
// when the config omits it.
func (r *guardrailResource) apply(m *guardrailResourceModel, a *guardrailAPI) error {
	m.ID = types.StringValue(a.ID)
	m.Name = types.StringValue(a.Name)
	m.Enabled = types.BoolValue(a.Enabled)

	// description is Optional-only: only reflect a non-nil/non-empty server value.
	if a.Description != nil && *a.Description != "" {
		m.Description = types.StringValue(*a.Description)
	}

	// Normalise rules to compact JSON for stable state storage.
	if len(a.Rules) > 0 {
		compact, err := compactJSON(a.Rules)
		if err != nil {
			return fmt.Errorf("normalise rules JSON: %w", err)
		}
		m.Rules = types.StringValue(compact)
	} else {
		m.Rules = types.StringValue("[]")
	}
	return nil
}

// ---------------------------------------------------------------------------
// JSON helpers
// ---------------------------------------------------------------------------
// compactJSON and parseJSONArray live in helpers.go (shared with flow_resource).
// parseRulesJSON is declared above as a guardrail-specific alias.

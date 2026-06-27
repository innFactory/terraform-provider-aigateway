package provider

// aigateway_flow manages a flow on the AI gateway.
//
// steps is modeled as a JSON-encoded string attribute (types.String) rather
// than nested blocks. FlowStep is a serde-tagged enum — the variants differ in
// which fields they carry (model has strategy+candidates, guardrail has a
// policy_id, route has conditions+branches, etc.). Encoding a tagged-union
// graph as nested blocks would require one block type per variant (verbose, not
// extensible) or a catch-all dynamic attribute (loses type-checking). A JSON
// string keeps the HCL concise, avoids provider-side schema churn when new step
// types are added, and lets the caller write/copy the exact JSON they'd POST to
// the admin API. The provider json.Unmarshal's the string into []any before
// sending so it is transmitted as a real JSON array, not a quoted string, on the
// wire.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                     = (*flowResource)(nil)
	_ resource.ResourceWithConfigure        = (*flowResource)(nil)
	_ resource.ResourceWithImportState      = (*flowResource)(nil)
	_ resource.ResourceWithConfigValidators = (*flowResource)(nil)
)

type flowResource struct {
	client *httpClient
}

func newFlowResource() resource.Resource {
	return &flowResource{}
}

type flowResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	// entry is the step id that the flow starts at.
	Entry types.String `tfsdk:"entry"`
	// steps is a JSON-encoded string representing []FlowStep.
	// See the package-level comment for the rationale.
	Steps types.String `tfsdk:"steps"`
}

func (r *flowResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_flow"
}

func (r *flowResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An orchestration flow on the AI gateway. " +
			"A flow is a directed graph of steps (model, guardrail, route, etc.) that " +
			"the gateway traverses when serving a request. Assign flows to scopes via " +
			"the gateway configuration. The steps graph is heterogeneous — each step " +
			"is a tagged-enum object — so it is modeled as a JSON-encoded string " +
			"validated server-side via POST /api/v1/admin/flows/validate.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Server-assigned flow id (uuid).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Unique name for this flow.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Optional human-readable description of the flow.",
			},
			"entry": schema.StringAttribute{
				Required:    true,
				Description: "The step id (string) at which the flow starts execution.",
			},
			// steps is a JSON-encoded array of FlowStep objects.
			// Each element must be a tagged-enum object with a required "type" field,
			// a required "id" field (the step's StepId), and type-specific fields, e.g.:
			//   [{"type":"model","id":"m1","strategy":{"type":"failover"},
			//     "candidates":[{"model_id":"<uuid>","weight":1,"priority":1}],"next":null},
			//    {"type":"guardrail","id":"g1","policy_ids":["<uuid>"],"on_block":"refuse","next":"m1"}]
			// The provider validates the JSON at plan time via the /validate endpoint
			// when the gateway is reachable, and always validates that the value is
			// well-formed JSON before sending.
			"steps": schema.StringAttribute{
				Required: true,
				Description: "JSON-encoded array of FlowStep objects. " +
					"Each step is a tagged-enum object with a required \"type\" field " +
					"(model | guardrail | route) and a required \"id\" field (StepId string). " +
					"Additional fields depend on the type. " +
					"Example: [{\"type\":\"model\",\"id\":\"m1\"," +
					"\"strategy\":{\"type\":\"failover\"}," +
					"\"candidates\":[{\"model_id\":\"<uuid>\",\"weight\":1,\"priority\":1}]," +
					"\"next\":null}]",
			},
		},
	}
}

func (r *flowResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*httpClient)
}

// ConfigValidators calls the gateway /validate endpoint at plan time so that
// step errors surface before apply. The check is best-effort: if the provider
// is not yet configured (nil client) we skip validation silently rather than
// failing the plan.
func (r *flowResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		&flowStepsValidator{client: &r.client},
	}
}

// flowStepsValidator is a resource.ConfigValidator that calls the gateway
// /validate endpoint. It holds a pointer-to-pointer so it always sees the
// client that Configure() stores on the resource after provider setup.
type flowStepsValidator struct {
	client **httpClient
}

func (v *flowStepsValidator) Description(_ context.Context) string {
	return "Validates flow steps against the gateway /validate endpoint."
}

func (v *flowStepsValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v *flowStepsValidator) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	// Skip if provider not configured yet.
	if v.client == nil || *v.client == nil {
		return
	}

	var m flowResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m.Steps.IsNull() || m.Steps.IsUnknown() {
		return
	}
	if m.Entry.IsNull() || m.Entry.IsUnknown() {
		return
	}

	steps, err := parseJSONArray(m.Steps.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("steps"), "Invalid steps JSON", err.Error())
		return
	}

	body := flowValidateRequest{
		Steps: steps,
		Entry: m.Entry.ValueString(),
	}
	if !m.Name.IsNull() && !m.Name.IsUnknown() {
		s := m.Name.ValueString()
		body.Name = &s
	}
	if !m.Description.IsNull() && !m.Description.IsUnknown() {
		s := m.Description.ValueString()
		body.Description = &s
	}

	var out flowValidateResponse
	if err := (*v.client).do(ctx, "POST", "/api/v1/admin/flows/validate", nil, body, &out); err != nil {
		// Validation endpoint unreachable — skip (don't block the plan).
		return
	}
	if !out.Valid {
		for _, e := range out.Errors {
			resp.Diagnostics.AddAttributeError(path.Root("steps"), "Invalid flow steps", e)
		}
	}
}

// ---------------------------------------------------------------------------
// Wire types
// ---------------------------------------------------------------------------

type flowCreateBody struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Steps       []any  `json:"steps"`
	Entry       string `json:"entry"`
}

type flowUpdateBody struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Steps       []any   `json:"steps,omitempty"`
	Entry       *string `json:"entry,omitempty"`
}

type flowAPI struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Steps       json.RawMessage `json:"steps"`
	Entry       string          `json:"entry"`
}

type flowValidateRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	Steps       []any   `json:"steps"`
	Entry       string  `json:"entry"`
}

type flowValidateResponse struct {
	Valid  bool     `json:"valid"`
	Errors []string `json:"errors"`
}

// ---------------------------------------------------------------------------
// CRUD
// ---------------------------------------------------------------------------

func (r *flowResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan flowResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	steps, err := parseJSONArray(plan.Steps.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("steps"), "Invalid steps JSON", err.Error())
		return
	}

	body := flowCreateBody{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
		Steps:       steps,
		Entry:       plan.Entry.ValueString(),
	}

	var out flowAPI
	if err := r.client.do(ctx, "POST", "/api/v1/admin/flows", nil, body, &out); err != nil {
		resp.Diagnostics.AddError("Create flow failed", err.Error())
		return
	}
	if err := r.apply(&plan, &out); err != nil {
		resp.Diagnostics.AddError("Apply flow response failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *flowResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state flowResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var out flowAPI
	err := r.client.do(ctx, "GET", "/api/v1/admin/flows/"+state.ID.ValueString(), nil, nil, &out)
	if isNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Read flow failed", err.Error())
		return
	}
	if err := r.apply(&state, &out); err != nil {
		resp.Diagnostics.AddError("Apply flow response failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *flowResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan flowResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	var state flowResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	steps, err := parseJSONArray(plan.Steps.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("steps"), "Invalid steps JSON", err.Error())
		return
	}

	name := plan.Name.ValueString()
	desc := plan.Description.ValueString()
	entry := plan.Entry.ValueString()
	body := flowUpdateBody{
		Name:        &name,
		Description: &desc,
		Steps:       steps,
		Entry:       &entry,
	}

	var out flowAPI
	if err := r.client.do(ctx, "PATCH", "/api/v1/admin/flows/"+state.ID.ValueString(), nil, body, &out); err != nil {
		resp.Diagnostics.AddError("Update flow failed", err.Error())
		return
	}
	plan.ID = state.ID
	if err := r.apply(&plan, &out); err != nil {
		resp.Diagnostics.AddError("Apply flow response failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *flowResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state flowResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.do(ctx, "DELETE", "/api/v1/admin/flows/"+state.ID.ValueString(), nil, nil, nil); err != nil && !isNotFound(err) {
		resp.Diagnostics.AddError("Delete flow failed", err.Error())
	}
}

func (r *flowResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// ---------------------------------------------------------------------------
// apply: maps an API response back onto the model
// ---------------------------------------------------------------------------

// apply copies the server response fields into m. steps is round-tripped as a
// compact JSON string. description is reflected from the server response
// (the gateway treats it as a required string with a default of "").
func (r *flowResource) apply(m *flowResourceModel, a *flowAPI) error {
	m.ID = types.StringValue(a.ID)
	m.Name = types.StringValue(a.Name)
	m.Entry = types.StringValue(a.Entry)

	// description: reflect the server value; only set null when not in config.
	// The flow API returns description as a non-optional string (default "").
	// We reflect it only when non-empty to stay consistent with optional fields.
	if a.Description != "" {
		m.Description = types.StringValue(a.Description)
	}

	// Normalise steps to compact JSON for stable state storage.
	// compactJSON handles nil/empty/null by returning "[]".
	compact, err := compactJSON(a.Steps)
	if err != nil {
		return fmt.Errorf("normalise steps JSON: %w", err)
	}
	m.Steps = types.StringValue(compact)
	return nil
}

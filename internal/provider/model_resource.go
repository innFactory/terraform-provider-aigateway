package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*modelResource)(nil)
	_ resource.ResourceWithConfigure   = (*modelResource)(nil)
	_ resource.ResourceWithImportState = (*modelResource)(nil)
)

type modelResource struct {
	client *httpClient
}

func newModelResource() resource.Resource {
	return &modelResource{}
}

type modelResourceModel struct {
	ModelID         types.String `tfsdk:"model_id"`
	DisplayName     types.String `tfsdk:"display_name"`
	ProviderID      types.String `tfsdk:"provider_id"`
	ProviderModelID types.String `tfsdk:"provider_model_id"`
	DeploymentName  types.String `tfsdk:"deployment_name"`
	Capability      types.String `tfsdk:"capability"`
	ModelType       types.String `tfsdk:"model_type"`
	InputMicros     types.Int64  `tfsdk:"input_per_1m_tokens_microdollars"`
	OutputMicros    types.Int64  `tfsdk:"output_per_1m_tokens_microdollars"`
	CachedMicros    types.Int64  `tfsdk:"cached_input_per_1m_tokens_microdollars"`
	Enabled         types.Bool   `tfsdk:"enabled"`
	IsDefault       types.Bool   `tfsdk:"is_default"`
	PriceRegion     types.String `tfsdk:"price_region"`
	ManagedBy       types.String `tfsdk:"managed_by"`
	ID              types.String `tfsdk:"id"`
}

func (r *modelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_model"
}

func (r *modelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A model exposed by the gateway, bound to an aigateway_provider. Identified by the caller-chosen model_id; the provider addresses the gateway by the server doc id (id, model_<uuid>) so the same model name may exist under multiple providers. Import by doc id, or by name when the name is unique.",
		Attributes: map[string]schema.Attribute{
			"model_id": schema.StringAttribute{
				Required:      true,
				Description:   "Stable model id clients call (e.g. gpt-5.4-mini). Immutable.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"display_name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable name shown in the dashboard.",
			},
			"provider_id": schema.StringAttribute{
				Required:    true,
				Description: "ID of the aigateway_provider that serves this model.",
			},
			"provider_model_id": schema.StringAttribute{
				Required:    true,
				Description: "The model id as the upstream provider knows it.",
			},
			"deployment_name": schema.StringAttribute{
				Optional:    true,
				Description: "Azure deployment name (required for azure_openai).",
			},
			// All of the following are Optional+Computed. UseStateForUnknown keeps
			// the prior state value when the config omits the attribute, instead
			// of planning "unknown" — which on an in-place update would be sent
			// as the zero value ("" / 0 / false), clobbering server-managed data.
			// Without it, e.g. editing display_name would zero the gateway-fetched
			// pricing, blank the capability, or disable the model.
			"capability": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "chat | embedding | image | audio. Defaults to chat.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"model_type": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "chat | embedding | image | audio. Defaults to chat.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"input_per_1m_tokens_microdollars": schema.Int64Attribute{
				Optional:      true,
				Computed:      true,
				Description:   "Input token price per 1M tokens in microdollars.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"output_per_1m_tokens_microdollars": schema.Int64Attribute{
				Optional:      true,
				Computed:      true,
				Description:   "Output token price per 1M tokens in microdollars.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"cached_input_per_1m_tokens_microdollars": schema.Int64Attribute{
				Optional:      true,
				Computed:      true,
				Description:   "Cached input token price per 1M tokens in microdollars.",
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"enabled": schema.BoolAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Whether the model is enabled. Defaults to true.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"is_default": schema.BoolAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Whether this is the tenant default model.",
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
			"price_region": schema.StringAttribute{
				Optional: true,
				Description: "ai-prices region.id to bill at for region/SKU-correct pricing " +
					"(e.g. global-standard, eu-data-zone-standard, us-standard, europe-west1). " +
					"When omitted, the gateway derives it from the deployment SKU / provider " +
					"region, falling back to the global price. Pin this for Azure DataZone " +
					"deployments so cache/token rates match the EU data-zone price.",
			},
			"managed_by": schema.StringAttribute{
				Optional:    true,
				Description: "Free-form marker stored on the gateway object (e.g. companygpt-terraform) so the UI can flag IaC-managed providers/models.",
			},
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Server-assigned internal doc id (model_<uuid>). Read/Update/Delete address the gateway by this id, so duplicate model names across providers stay unambiguous.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *modelResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*httpClient)
}

type modelCreateBody struct {
	ModelID         string  `json:"modelId"`
	DisplayName     string  `json:"displayName"`
	ProviderID      string  `json:"providerId"`
	ProviderModelID string  `json:"providerModelId"`
	DeploymentName  *string `json:"deploymentName,omitempty"`
	Capability      string  `json:"capability"`
	ModelType       string  `json:"modelType"`
	InputMicros     int64   `json:"inputPer1mTokensMicrodollars"`
	OutputMicros    int64   `json:"outputPer1mTokensMicrodollars"`
	CachedMicros    int64   `json:"cachedInputPer1mTokensMicrodollars"`
	Enabled         bool    `json:"enabled"`
	IsDefault       bool    `json:"isDefault"`
	PriceRegion     *string `json:"priceRegion,omitempty"`
	ManagedBy       *string `json:"managedBy,omitempty"`
}

type modelUpdateBody struct {
	DisplayName     *string `json:"displayName,omitempty"`
	ProviderID      *string `json:"providerId,omitempty"`
	ProviderModelID *string `json:"providerModelId,omitempty"`
	DeploymentName  *string `json:"deploymentName,omitempty"`
	InputMicros     *int64  `json:"inputPer1mTokensMicrodollars,omitempty"`
	OutputMicros    *int64  `json:"outputPer1mTokensMicrodollars,omitempty"`
	CachedMicros    *int64  `json:"cachedInputPer1mTokensMicrodollars,omitempty"`
	Enabled         *bool   `json:"enabled,omitempty"`
	IsDefault       *bool   `json:"isDefault,omitempty"`
	PriceRegion     *string `json:"priceRegion,omitempty"`
	ManagedBy       *string `json:"managedBy,omitempty"`
}

type modelAPI struct {
	ID              string  `json:"id"`
	ModelID         string  `json:"modelId"`
	DisplayName     string  `json:"displayName"`
	ProviderID      string  `json:"providerId"`
	ProviderModelID string  `json:"providerModelId"`
	DeploymentName  string  `json:"deploymentName"`
	Capability      string  `json:"capability"`
	ModelType       string  `json:"modelType"`
	InputMicros     int64   `json:"inputPer1mTokensMicrodollars"`
	OutputMicros    int64   `json:"outputPer1mTokensMicrodollars"`
	CachedMicros    int64   `json:"cachedInputPer1mTokensMicrodollars"`
	Enabled         bool    `json:"enabled"`
	IsDefault       bool    `json:"isDefault"`
	PriceRegion     *string `json:"priceRegion"`
	ManagedBy       string  `json:"managedBy"`
}

func defStr(v types.String, def string) string {
	if s := optString(v); s != "" {
		return s
	}
	return def
}

// defBool returns def when v is unset — i.e. null OR unknown. enabled/is_default
// are Optional+Computed, so an unset config value plans as UNKNOWN (not null);
// checking only IsNull would yield false here and ValueBool() would return the
// zero value, which silently created models disabled.
func defBool(v types.Bool, def bool) bool {
	if v.IsNull() || v.IsUnknown() {
		return def
	}
	return v.ValueBool()
}

func (r *modelResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan modelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cap := defStr(plan.Capability, "chat")
	body := modelCreateBody{
		ModelID:         plan.ModelID.ValueString(),
		DisplayName:     plan.DisplayName.ValueString(),
		ProviderID:      plan.ProviderID.ValueString(),
		ProviderModelID: plan.ProviderModelID.ValueString(),
		DeploymentName:  ptrIf(plan.DeploymentName),
		Capability:      cap,
		ModelType:       defStr(plan.ModelType, cap),
		InputMicros:     plan.InputMicros.ValueInt64(),
		OutputMicros:    plan.OutputMicros.ValueInt64(),
		CachedMicros:    plan.CachedMicros.ValueInt64(),
		Enabled:         defBool(plan.Enabled, true),
		IsDefault:       defBool(plan.IsDefault, false),
		PriceRegion:     ptrIf(plan.PriceRegion),
		ManagedBy:       ptrIf(plan.ManagedBy),
	}
	var out modelAPI
	err := r.client.do(ctx, "POST", "/api/v1/admin/models", nil, body, &out)
	if isConflict(err) {
		// ADOPT-ON-CONFLICT: a doc for this (provider, model_id) already
		// exists on the gateway but is missing from Terraform state — the
		// canonical way this happens is a state prune from a version-skewed
		// refresh (e.g. provider v0.8.2 reading by doc id against a gateway
		// that still resolved names only). These models are declared
		// self-healing (managed_by=companygpt-terraform), so create adopts
		// the existing doc and aligns it to the plan instead of failing.
		adopted, aerr := r.adoptExistingModel(ctx, &plan, body)
		if aerr != nil {
			resp.Diagnostics.AddError("Create model failed",
				fmt.Sprintf("gateway reports the model exists but adopting it failed: %s (original conflict: %s)", aerr, err))
			return
		}
		out = *adopted
	} else if err != nil {
		resp.Diagnostics.AddError("Create model failed", err.Error())
		return
	}
	r.apply(&plan, &out)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// adoptExistingModel resolves the existing doc for the plan's
// (provider_id, model_id) via the list endpoint and PUTs the planned
// attributes onto it, returning the aligned doc.
func (r *modelResource) adoptExistingModel(ctx context.Context, plan *modelResourceModel, body modelCreateBody) (*modelAPI, error) {
	var list []modelAPI
	if err := r.client.do(ctx, "GET", "/api/v1/admin/models", nil, nil, &list); err != nil {
		return nil, fmt.Errorf("listing models: %w", err)
	}
	var match *modelAPI
	for i := range list {
		if list[i].ModelID == body.ModelID && list[i].ProviderID == body.ProviderID {
			if match != nil {
				return nil, fmt.Errorf("more than one doc for model %q under provider %q — clean up duplicates first", body.ModelID, body.ProviderID)
			}
			match = &list[i]
		}
	}
	if match == nil {
		return nil, fmt.Errorf("no doc for model %q under provider %q (the conflicting doc belongs to another provider — pick a different model_id or import that doc)", body.ModelID, body.ProviderID)
	}
	upd := modelUpdateBody{
		DisplayName:     &body.DisplayName,
		ProviderID:      &body.ProviderID,
		ProviderModelID: &body.ProviderModelID,
		DeploymentName:  body.DeploymentName,
		InputMicros:     &body.InputMicros,
		OutputMicros:    &body.OutputMicros,
		CachedMicros:    &body.CachedMicros,
		Enabled:         &body.Enabled,
		IsDefault:       &body.IsDefault,
		PriceRegion:     body.PriceRegion,
		ManagedBy:       body.ManagedBy,
	}
	var out modelAPI
	if err := r.client.do(ctx, "PUT", "/api/v1/admin/models/"+match.ID, nil, upd, &out); err != nil {
		return nil, fmt.Errorf("aligning adopted model %s: %w", match.ID, err)
	}
	return &out, nil
}

// modelAdminPath returns the admin API path for a model, preferring the
// server-assigned doc id (model_<uuid>) over the caller-chosen model_id NAME.
// The same model name may exist under several providers (one catalog row per
// provider); gateway >= v0.16.16 resolves `{model_id}` doc-id-first and
// answers 409 for a by-name request that matches more than one model, so the
// doc id is the only always-unambiguous handle. The name is used only when no
// id is recorded in state yet (e.g. imported by name with an older provider
// version and never refreshed). byName reports which handle was chosen so
// callers can decorate a 409 with the re-import hint.
func modelAdminPath(id, name types.String) (p string, byName bool) {
	if s := id.ValueString(); !id.IsNull() && !id.IsUnknown() && s != "" {
		return "/api/v1/admin/models/" + s, false
	}
	return "/api/v1/admin/models/" + name.ValueString(), true
}

// modelErrDetail expands a gateway 409 raised on a BY-NAME model request into
// an actionable message; every other error passes through unchanged.
func modelErrDetail(err error, byName bool) string {
	if byName && isConflict(err) {
		return "model name is ambiguous across providers — this provider version addresses models by their server doc id (model_<uuid>), but state has no id recorded; re-import the resource by doc id (terraform import <address> model_<uuid>) so operations are unambiguous: " + err.Error()
	}
	return err.Error()
}

func (r *modelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state modelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, byName := modelAdminPath(state.ID, state.ModelID)
	var out modelAPI
	err := r.client.do(ctx, "GET", p, nil, nil, &out)
	if isNotFound(err) && !byName {
		// Version-skew guard: a gateway older than v0.16.16 resolves the
		// {model_id} path segment by NAME only, so a by-doc-id read 404s
		// even though the model exists. Retry by name before concluding the
		// resource is gone — dropping it from state here caused create/409
		// storms on the next apply.
		nameP := "/api/v1/admin/models/" + state.ModelID.ValueString()
		err = r.client.do(ctx, "GET", nameP, nil, nil, &out)
		byName = true
	}
	if isNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Read model failed", modelErrDetail(err, byName))
		return
	}
	r.apply(&state, &out)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *modelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state modelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	dn := plan.DisplayName.ValueString()
	pid := plan.ProviderID.ValueString()
	pmid := plan.ProviderModelID.ValueString()
	in := plan.InputMicros.ValueInt64()
	out64 := plan.OutputMicros.ValueInt64()
	cached := plan.CachedMicros.ValueInt64()
	enabled := defBool(plan.Enabled, true)
	isDefault := defBool(plan.IsDefault, false)
	body := modelUpdateBody{
		DisplayName:     &dn,
		ProviderID:      &pid,
		ProviderModelID: &pmid,
		DeploymentName:  ptrIf(plan.DeploymentName),
		InputMicros:     &in,
		OutputMicros:    &out64,
		CachedMicros:    &cached,
		Enabled:         &enabled,
		IsDefault:       &isDefault,
		PriceRegion:     ptrIf(plan.PriceRegion),
		ManagedBy:       ptrIf(plan.ManagedBy),
	}
	// Address by the doc id from state (plan.ID may be unknown mid-plan);
	// model_id is immutable (RequiresReplace), so the state's name is the
	// correct fallback too.
	p, byName := modelAdminPath(state.ID, plan.ModelID)
	var out modelAPI
	if err := r.client.do(ctx, "PUT", p, nil, body, &out); err != nil {
		resp.Diagnostics.AddError("Update model failed", modelErrDetail(err, byName))
		return
	}
	r.apply(&plan, &out)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *modelResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state modelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, byName := modelAdminPath(state.ID, state.ModelID)
	if err := r.client.do(ctx, "DELETE", p, nil, nil, nil); err != nil && !isNotFound(err) {
		resp.Diagnostics.AddError("Delete model failed", modelErrDetail(err, byName))
	}
}

// ImportState accepts either handle:
//   - the server doc id (model_<uuid>) — preferred, always unambiguous;
//   - the caller-chosen model_id NAME — resolved to its doc id via the list
//     endpoint, accepted only when exactly one model carries that name.
func (r *modelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if strings.HasPrefix(req.ID, "model_") {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
		return
	}
	var list []modelAPI
	if err := r.client.do(ctx, "GET", "/api/v1/admin/models", nil, nil, &list); err != nil {
		resp.Diagnostics.AddError("Import model failed",
			fmt.Sprintf("listing models to resolve name %q: %s", req.ID, err.Error()))
		return
	}
	docID, err := resolveModelImportID(list, req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Import model failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("model_id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), docID)...)
}

// resolveModelImportID maps an import identifier that is a model NAME to the
// unique server doc id. The name must match exactly one model; a name shared
// across providers is ambiguous and must be imported by doc id instead.
func resolveModelImportID(list []modelAPI, name string) (string, error) {
	var matches []modelAPI
	for i := range list {
		if list[i].ModelID == name {
			matches = append(matches, list[i])
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no model with model_id %q exists on the gateway", name)
	case 1:
		return matches[0].ID, nil
	default:
		ids := make([]string, len(matches))
		for i := range matches {
			ids[i] = fmt.Sprintf("%s (provider %s)", matches[i].ID, matches[i].ProviderID)
		}
		return "", fmt.Errorf(
			"model name %q is ambiguous — it exists under multiple providers: %s; import by server doc id instead (terraform import <address> model_<uuid>)",
			name, strings.Join(ids, ", "))
	}
}

func (r *modelResource) apply(m *modelResourceModel, a *modelAPI) {
	m.ID = types.StringValue(a.ID)
	m.ModelID = types.StringValue(a.ModelID)
	m.DisplayName = types.StringValue(a.DisplayName)
	m.ProviderID = types.StringValue(a.ProviderID)
	m.ProviderModelID = types.StringValue(a.ProviderModelID)
	if a.DeploymentName != "" {
		m.DeploymentName = types.StringValue(a.DeploymentName)
	}
	m.Capability = types.StringValue(a.Capability)
	m.ModelType = types.StringValue(a.ModelType)
	m.InputMicros = types.Int64Value(a.InputMicros)
	m.OutputMicros = types.Int64Value(a.OutputMicros)
	m.CachedMicros = types.Int64Value(a.CachedMicros)
	m.Enabled = types.BoolValue(a.Enabled)
	m.IsDefault = types.BoolValue(a.IsDefault)
	// price_region is Optional (not Computed): only reflect a server value when
	// the response carries one, otherwise keep the planned/null value to avoid
	// "inconsistent result" errors when unset.
	if a.PriceRegion != nil && *a.PriceRegion != "" {
		m.PriceRegion = types.StringValue(*a.PriceRegion)
	}
	// managed_by is Optional (not Computed): only reflect a server value when the
	// response carries one, otherwise keep the planned/null value to avoid
	// "inconsistent result" errors when unset. No else StringNull() here.
	if a.ManagedBy != "" {
		m.ManagedBy = types.StringValue(a.ManagedBy)
	}
}

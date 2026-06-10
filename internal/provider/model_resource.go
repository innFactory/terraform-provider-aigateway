package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
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
	ID              types.String `tfsdk:"id"`
}

func (r *modelResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_model"
}

func (r *modelResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A model exposed by the gateway, bound to an aigateway_provider. Keyed on the caller-chosen model_id.",
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
			"capability": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "chat | embedding | image | audio. Defaults to chat.",
			},
			"model_type": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "chat | embedding | image | audio. Defaults to chat.",
			},
			"input_per_1m_tokens_microdollars": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "Input token price per 1M tokens in microdollars.",
			},
			"output_per_1m_tokens_microdollars": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "Output token price per 1M tokens in microdollars.",
			},
			"cached_input_per_1m_tokens_microdollars": schema.Int64Attribute{
				Optional:    true,
				Computed:    true,
				Description: "Cached input token price per 1M tokens in microdollars.",
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the model is enabled. Defaults to true.",
			},
			"is_default": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether this is the tenant default model.",
			},
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Server-assigned internal id (model_<uuid>).",
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
}

type modelAPI struct {
	ID              string `json:"id"`
	ModelID         string `json:"modelId"`
	DisplayName     string `json:"displayName"`
	ProviderID      string `json:"providerId"`
	ProviderModelID string `json:"providerModelId"`
	DeploymentName  string `json:"deploymentName"`
	Capability      string `json:"capability"`
	ModelType       string `json:"modelType"`
	InputMicros     int64  `json:"inputPer1mTokensMicrodollars"`
	OutputMicros    int64  `json:"outputPer1mTokensMicrodollars"`
	CachedMicros    int64  `json:"cachedInputPer1mTokensMicrodollars"`
	Enabled         bool   `json:"enabled"`
	IsDefault       bool   `json:"isDefault"`
}

func defStr(v types.String, def string) string {
	if s := optString(v); s != "" {
		return s
	}
	return def
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
		Enabled:         plan.Enabled.IsNull() || plan.Enabled.ValueBool(),
		IsDefault:       plan.IsDefault.ValueBool(),
	}
	var out modelAPI
	if err := r.client.do(ctx, "POST", "/api/v1/admin/models", nil, body, &out); err != nil {
		resp.Diagnostics.AddError("Create model failed", err.Error())
		return
	}
	r.apply(&plan, &out)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *modelResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state modelResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out modelAPI
	err := r.client.do(ctx, "GET", "/api/v1/admin/models/"+state.ModelID.ValueString(), nil, nil, &out)
	if isNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Read model failed", err.Error())
		return
	}
	r.apply(&state, &out)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *modelResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan modelResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	dn := plan.DisplayName.ValueString()
	pid := plan.ProviderID.ValueString()
	pmid := plan.ProviderModelID.ValueString()
	in := plan.InputMicros.ValueInt64()
	out64 := plan.OutputMicros.ValueInt64()
	cached := plan.CachedMicros.ValueInt64()
	enabled := plan.Enabled.ValueBool()
	isDefault := plan.IsDefault.ValueBool()
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
	}
	var out modelAPI
	if err := r.client.do(ctx, "PUT", "/api/v1/admin/models/"+plan.ModelID.ValueString(), nil, body, &out); err != nil {
		resp.Diagnostics.AddError("Update model failed", err.Error())
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
	if err := r.client.do(ctx, "DELETE", "/api/v1/admin/models/"+state.ModelID.ValueString(), nil, nil, nil); err != nil && !isNotFound(err) {
		resp.Diagnostics.AddError("Delete model failed", err.Error())
	}
}

func (r *modelResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("model_id"), req.ID)...)
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
}

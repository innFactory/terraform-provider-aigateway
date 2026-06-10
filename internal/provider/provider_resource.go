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
	_ resource.Resource                = (*providerResource)(nil)
	_ resource.ResourceWithConfigure   = (*providerResource)(nil)
	_ resource.ResourceWithImportState = (*providerResource)(nil)
)

type providerResource struct {
	client *httpClient
}

func newProviderResource() resource.Resource {
	return &providerResource{}
}

type providerResourceModel struct {
	ID         types.String `tfsdk:"id"`
	Type       types.String `tfsdk:"type"`
	Name       types.String `tfsdk:"name"`
	Endpoint   types.String `tfsdk:"endpoint"`
	AuthType   types.String `tfsdk:"auth_type"`
	Credential types.String `tfsdk:"credential"`
	Region     types.String `tfsdk:"region"`
	ProjectID  types.String `tfsdk:"project_id"`
	APIVersion types.String `tfsdk:"api_version"`
	Enabled    types.Bool   `tfsdk:"enabled"`
}

func (r *providerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_provider"
}

func (r *providerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An upstream AI provider (openai, azure_openai, anthropic, gemini, ...) configured on the gateway.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Server-assigned provider id (provider_<uuid>).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"type": schema.StringAttribute{
				Required:      true,
				Description:   "Provider type: openai | azure_openai | anthropic | gemini | mistral | stackit | aws_bedrock | custom_openai | ollama.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable provider name.",
			},
			"endpoint": schema.StringAttribute{
				Required:    true,
				Description: "Upstream API base URL.",
			},
			"auth_type": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Authentication type (apiKey | managedIdentity | none). Defaults to apiKey.",
			},
			"credential": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Raw upstream API key / secret. Stored in the gateway credential store; never read back.",
			},
			"region": schema.StringAttribute{
				Optional:    true,
				Description: "Region (Vertex AI, AWS Bedrock).",
			},
			"project_id": schema.StringAttribute{
				Optional:    true,
				Description: "GCP project id (Vertex AI).",
			},
			"api_version": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "API version (Azure OpenAI).",
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the provider is enabled. Defaults to true.",
			},
		},
	}
}

func (r *providerResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*httpClient)
}

type providerCreateBody struct {
	Type       string  `json:"type"`
	Name       string  `json:"name"`
	Endpoint   string  `json:"endpoint"`
	AuthType   string  `json:"authType"`
	Credential *string `json:"credential,omitempty"`
	Region     *string `json:"region,omitempty"`
	ProjectID  *string `json:"projectId,omitempty"`
	APIVersion *string `json:"apiVersion,omitempty"`
}

type providerUpdateBody struct {
	Name       *string `json:"name,omitempty"`
	Endpoint   *string `json:"endpoint,omitempty"`
	Credential *string `json:"credential,omitempty"`
	Region     *string `json:"region,omitempty"`
	ProjectID  *string `json:"projectId,omitempty"`
	APIVersion *string `json:"apiVersion,omitempty"`
	Enabled    *bool   `json:"enabled,omitempty"`
}

type providerAPI struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Name       string `json:"name"`
	Endpoint   string `json:"endpoint"`
	AuthType   string `json:"authType"`
	Region     string `json:"region"`
	ProjectID  string `json:"projectId"`
	APIVersion string `json:"apiVersion"`
	Enabled    bool   `json:"enabled"`
}

func ptrIf(v types.String) *string {
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		return nil
	}
	s := v.ValueString()
	return &s
}

func (r *providerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan providerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	authType := optString(plan.AuthType)
	if authType == "" {
		authType = "apiKey"
	}
	body := providerCreateBody{
		Type:       plan.Type.ValueString(),
		Name:       plan.Name.ValueString(),
		Endpoint:   plan.Endpoint.ValueString(),
		AuthType:   authType,
		Credential: ptrIf(plan.Credential),
		Region:     ptrIf(plan.Region),
		ProjectID:  ptrIf(plan.ProjectID),
		APIVersion: ptrIf(plan.APIVersion),
	}
	var out providerAPI
	if err := r.client.do(ctx, "POST", "/api/v1/admin/providers", nil, body, &out); err != nil {
		resp.Diagnostics.AddError("Create provider failed", err.Error())
		return
	}
	r.apply(&plan, &out)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *providerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state providerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var list []providerAPI
	if err := r.client.do(ctx, "GET", "/api/v1/admin/providers", nil, nil, &list); err != nil {
		resp.Diagnostics.AddError("Read provider failed", err.Error())
		return
	}
	id := state.ID.ValueString()
	for i := range list {
		if list[i].ID == id {
			r.apply(&state, &list[i])
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *providerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state providerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	enabled := plan.Enabled.ValueBool()
	body := providerUpdateBody{
		Name:       ptrIf(plan.Name),
		Endpoint:   ptrIf(plan.Endpoint),
		Credential: ptrIf(plan.Credential),
		Region:     ptrIf(plan.Region),
		ProjectID:  ptrIf(plan.ProjectID),
		APIVersion: ptrIf(plan.APIVersion),
		Enabled:    &enabled,
	}
	var out providerAPI
	if err := r.client.do(ctx, "PATCH", "/api/v1/admin/providers/"+state.ID.ValueString(), nil, body, &out); err != nil {
		resp.Diagnostics.AddError("Update provider failed", err.Error())
		return
	}
	plan.ID = state.ID
	r.apply(&plan, &out)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *providerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state providerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.do(ctx, "DELETE", "/api/v1/admin/providers/"+state.ID.ValueString()+"?force=true", nil, nil, nil); err != nil && !isNotFound(err) {
		resp.Diagnostics.AddError("Delete provider failed", err.Error())
	}
}

func (r *providerResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// apply copies API fields into the model (credential is never read back).
func (r *providerResource) apply(m *providerResourceModel, a *providerAPI) {
	m.ID = types.StringValue(a.ID)
	m.Type = types.StringValue(a.Type)
	m.Name = types.StringValue(a.Name)
	m.Endpoint = types.StringValue(a.Endpoint)
	m.AuthType = types.StringValue(a.AuthType)
	m.Enabled = types.BoolValue(a.Enabled)
	if a.Region != "" {
		m.Region = types.StringValue(a.Region)
	}
	if a.ProjectID != "" {
		m.ProjectID = types.StringValue(a.ProjectID)
	}
	if a.APIVersion != "" {
		m.APIVersion = types.StringValue(a.APIVersion)
	}
}

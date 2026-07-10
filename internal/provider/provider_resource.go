package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	aigateway "github.com/innFactory/aigateway-go"
)

var (
	_ resource.Resource                = (*providerResource)(nil)
	_ resource.ResourceWithConfigure   = (*providerResource)(nil)
	_ resource.ResourceWithImportState = (*providerResource)(nil)
)

type providerResource struct {
	client *aigateway.Client
}

func newProviderResource() resource.Resource {
	return &providerResource{}
}

type providerResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Type         types.String `tfsdk:"type"`
	Name         types.String `tfsdk:"name"`
	Endpoint     types.String `tfsdk:"endpoint"`
	AuthType     types.String `tfsdk:"auth_type"`
	Credential   types.String `tfsdk:"credential"`
	Region       types.String `tfsdk:"region"`
	ProjectID    types.String `tfsdk:"project_id"`
	APIVersion   types.String `tfsdk:"api_version"`
	Enabled      types.Bool   `tfsdk:"enabled"`
	ManagedBy    types.String `tfsdk:"managed_by"`
	InferenceGeo types.String `tfsdk:"inference_geo"`
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
				// Optional+Computed: when the config omits it, keep the prior
				// state value instead of planning "unknown". Without this the
				// planned value goes unknown on any in-place update and is sent
				// as "" (the zero value), clobbering the server default.
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
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
				Optional:      true,
				Computed:      true,
				Description:   "API version (Azure OpenAI).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"managed_by": schema.StringAttribute{
				Optional:    true,
				Description: "Free-form marker stored on the gateway object (e.g. companygpt-terraform) so the UI can flag IaC-managed providers/models.",
			},
			"inference_geo": schema.StringAttribute{
				Optional:    true,
				Description: "AWS Bedrock cross-region inference geo prefix (eu | us | apac | au | jp | global). When set, model IDs are auto-prefixed (e.g. eu.anthropic.claude-sonnet-4-6). Only applies to aws_bedrock providers.",
			},
			"enabled": schema.BoolAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the provider is enabled. Defaults to true.",
				// CRITICAL: without UseStateForUnknown, an in-place update of any
				// other attribute (e.g. api_version) plans `enabled` as unknown,
				// so Update sends enabled=false (the bool zero value) and silently
				// DISABLES the provider. Keeping the prior state value prevents
				// that — a provider stays enabled across unrelated updates.
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *providerResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*aigateway.Client)
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
		Type:         plan.Type.ValueString(),
		Name:         plan.Name.ValueString(),
		Endpoint:     plan.Endpoint.ValueString(),
		AuthType:     authType,
		Credential:   ptrIf(plan.Credential),
		Region:       ptrIf(plan.Region),
		ProjectID:    ptrIf(plan.ProjectID),
		APIVersion:   ptrIf(plan.APIVersion),
		ManagedBy:    ptrIf(plan.ManagedBy),
		InferenceGeo: ptrIf(plan.InferenceGeo),
	}
	out, err := r.client.CreateProvider(ctx, body)
	if err != nil {
		resp.Diagnostics.AddError("Create provider failed", err.Error())
		return
	}
	r.apply(&plan, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *providerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state providerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	list, err := r.client.ListProviders(ctx)
	if err != nil {
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
		Name:         ptrIf(plan.Name),
		Endpoint:     ptrIf(plan.Endpoint),
		Credential:   ptrIf(plan.Credential),
		Region:       ptrIf(plan.Region),
		ProjectID:    ptrIf(plan.ProjectID),
		APIVersion:   ptrIf(plan.APIVersion),
		ManagedBy:    ptrIf(plan.ManagedBy),
		InferenceGeo: ptrIf(plan.InferenceGeo),
		Enabled:      &enabled,
	}
	out, err := r.client.UpdateProvider(ctx, state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Update provider failed", err.Error())
		return
	}
	plan.ID = state.ID
	r.apply(&plan, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *providerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state providerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteProvider(ctx, state.ID.ValueString()); err != nil && !isNotFound(err) {
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
	// managed_by is Optional (not Computed): only reflect a server value when the
	// response carries one, otherwise keep the planned/null value to avoid
	// "inconsistent result" errors when unset. No else StringNull() here.
	if a.ManagedBy != "" {
		m.ManagedBy = types.StringValue(a.ManagedBy)
	}
	// inference_geo is Optional (not Computed): mirror the same pattern as
	// managed_by — only echo the server value when the field is set, so
	// operators who omit it don't see a spurious null→"" diff.
	if a.InferenceGeo != "" {
		m.InferenceGeo = types.StringValue(a.InferenceGeo)
	}
	// api_version is Optional+Computed: when unset in config (every non-Azure
	// provider) its planned value is unknown, so we MUST write a known value
	// here or Terraform rejects the result ("unknown value ... after apply").
	// Azure sets it and the gateway echoes it; everyone else gets null.
	if a.APIVersion != "" {
		m.APIVersion = types.StringValue(a.APIVersion)
	} else {
		m.APIVersion = types.StringNull()
	}
}

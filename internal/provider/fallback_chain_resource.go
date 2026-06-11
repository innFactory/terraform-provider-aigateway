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
	_ resource.Resource                = (*fallbackChainResource)(nil)
	_ resource.ResourceWithConfigure   = (*fallbackChainResource)(nil)
	_ resource.ResourceWithImportState = (*fallbackChainResource)(nil)
)

type fallbackChainResource struct {
	client *httpClient
}

func newFallbackChainResource() resource.Resource {
	return &fallbackChainResource{}
}

type fallbackChainResourceModel struct {
	ModelID        types.String `tfsdk:"model_id"`
	FallbackModels types.List   `tfsdk:"fallback_models"`
}

func (r *fallbackChainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_fallback_chain"
}

func (r *fallbackChainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Ordered fallback chain for a model: model_ids tried after this model's own deployments are exhausted. The gateway validates the chain (cycles, depth, referenced-model existence).",
		Attributes: map[string]schema.Attribute{
			"model_id": schema.StringAttribute{
				Required:      true,
				Description:   "The model whose fallback chain this manages.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"fallback_models": schema.ListAttribute{
				Required:    true,
				ElementType: types.StringType,
				Description: "Ordered list of model_ids to fall back to. Empty clears the chain.",
			},
		},
	}
}

func (r *fallbackChainResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*httpClient)
}

type fallbackChainBody struct {
	FallbackModels []string `json:"fallbackModels"`
}

func (r *fallbackChainResource) write(ctx context.Context, m *fallbackChainResourceModel) error {
	body := fallbackChainBody{FallbackModels: listOrNil(ctx, m.FallbackModels)}
	if body.FallbackModels == nil {
		body.FallbackModels = []string{}
	}
	return r.client.do(ctx, "PUT", "/api/v1/admin/models/"+m.ModelID.ValueString()+"/fallback", nil, body, nil)
}

func (r *fallbackChainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan fallbackChainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.write(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Set fallback chain failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *fallbackChainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state fallbackChainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out []string
	err := r.client.do(ctx, "GET", "/api/v1/admin/models/"+state.ModelID.ValueString()+"/fallback", nil, nil, &out)
	if isNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Read fallback chain failed", err.Error())
		return
	}
	state.FallbackModels = strList(ctx, &resp.Diagnostics, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *fallbackChainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan fallbackChainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.write(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Update fallback chain failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *fallbackChainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state fallbackChainResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Clear the chain (empty list).
	body := fallbackChainBody{FallbackModels: []string{}}
	if err := r.client.do(ctx, "PUT", "/api/v1/admin/models/"+state.ModelID.ValueString()+"/fallback", nil, body, nil); err != nil && !isNotFound(err) {
		resp.Diagnostics.AddError("Clear fallback chain failed", err.Error())
	}
}

func (r *fallbackChainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("model_id"), req.ID)...)
}

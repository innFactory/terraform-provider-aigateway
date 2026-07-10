package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	aigateway "github.com/innFactory/aigateway-go"
)

var (
	_ resource.Resource                = (*fallbackChainResource)(nil)
	_ resource.ResourceWithConfigure   = (*fallbackChainResource)(nil)
	_ resource.ResourceWithImportState = (*fallbackChainResource)(nil)
)

type fallbackChainResource struct {
	client *aigateway.Client
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
				Description:   "The model whose fallback chain this manages. Accepts the caller-chosen model_id or the server doc id (model_<uuid>); use aigateway_model.<name>.id when the same model name exists under multiple providers.",
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
	r.client = req.ProviderData.(*aigateway.Client)
}

func (r *fallbackChainResource) write(ctx context.Context, m *fallbackChainResourceModel) error {
	return r.client.SetFallbackChain(ctx, m.ModelID.ValueString(), listOrNil(ctx, m.FallbackModels))
}

func (r *fallbackChainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan fallbackChainResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.write(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Set fallback chain failed", ambiguousModelRefDetail(err))
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
	out, err := r.client.GetFallbackChain(ctx, state.ModelID.ValueString())
	if isNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Read fallback chain failed", ambiguousModelRefDetail(err))
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
		resp.Diagnostics.AddError("Update fallback chain failed", ambiguousModelRefDetail(err))
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
	if err := r.client.SetFallbackChain(ctx, state.ModelID.ValueString(), []string{}); err != nil && !isNotFound(err) {
		resp.Diagnostics.AddError("Clear fallback chain failed", ambiguousModelRefDetail(err))
	}
}

func (r *fallbackChainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("model_id"), req.ID)...)
}

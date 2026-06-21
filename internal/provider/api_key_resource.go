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
	_ resource.Resource                = (*apiKeyResource)(nil)
	_ resource.ResourceWithConfigure   = (*apiKeyResource)(nil)
	_ resource.ResourceWithImportState = (*apiKeyResource)(nil)
)

type apiKeyResource struct {
	client *httpClient
}

func newAPIKeyResource() resource.Resource {
	return &apiKeyResource{}
}

type apiKeyResourceModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	BudgetMicros     types.Int64  `tfsdk:"budget_microdollars"`
	AllowedModels    types.List   `tfsdk:"allowed_models"`
	AllowedProviders types.List   `tfsdk:"allowed_providers"`
	RateLimitRPM     types.Int64  `tfsdk:"rate_limit_rpm"`
	CostCenterID     types.String `tfsdk:"cost_center_id"`
	Key              types.String `tfsdk:"key"`
	KeyPrefix        types.String `tfsdk:"key_prefix"`
	Status           types.String `tfsdk:"status"`
}

func (r *apiKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_key"
}

func (r *apiKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A gateway API key. The plaintext `key` is returned exactly once at create time — store it as a sensitive output.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Server-assigned key id (key_<uuid>).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Human-readable key name.",
			},
			"budget_microdollars": schema.Int64Attribute{
				Optional:    true,
				Description: "Monthly budget cap in microdollars. Omit / null for unlimited.",
			},
			"allowed_models": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Restrict the key to these model ids. Empty = all tenant-allowed models.",
			},
			"allowed_providers": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Restrict the key to these provider ids. Empty = all.",
			},
			"rate_limit_rpm": schema.Int64Attribute{
				Optional:    true,
				Description: "Requests-per-minute limit for the key.",
			},
			"cost_center_id": schema.StringAttribute{
				Optional:    true,
				Description: "Cost center (budget) id this key's usage is attributed to. References an aigateway_cost_center id.",
			},
			"key": schema.StringAttribute{
				Computed:      true,
				Sensitive:     true,
				Description:   "The full plaintext key (aig_…). Available only right after create; null after import.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"key_prefix": schema.StringAttribute{
				Computed:    true,
				Description: "Key prefix shown in listings.",
			},
			"status": schema.StringAttribute{
				Computed:    true,
				Description: "active | revoked | expired.",
			},
		},
	}
}

func (r *apiKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*httpClient)
}

type apiKeyCreateBody struct {
	Name             string   `json:"name"`
	BudgetMicros     *int64   `json:"budgetMicrodollars,omitempty"`
	AllowedModels    []string `json:"allowedModels,omitempty"`
	AllowedProviders []string `json:"allowedProviders,omitempty"`
	RateLimitRPM     *int64   `json:"rateLimitRpm,omitempty"`
	CostCenterID     *string  `json:"costCenterId,omitempty"`
}

type apiKeyCreateResponse struct {
	ID        string `json:"id"`
	KeyPrefix string `json:"keyPrefix"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	RawKey    string `json:"rawKey"`
}

type apiKeyListEntry struct {
	ID               string   `json:"id"`
	KeyPrefix        string   `json:"keyPrefix"`
	Name             string   `json:"name"`
	Status           string   `json:"status"`
	AllowedModels    []string `json:"allowedModels"`
	AllowedProviders []string `json:"allowedProviders"`
	BudgetMicros     *int64   `json:"budgetMicrodollars"`
	RateLimitRPM     *int64   `json:"rateLimitRpm"`
	BudgetID         *string  `json:"budgetId"`
}

func listOrNil(ctx context.Context, l types.List) []string {
	if l.IsNull() || l.IsUnknown() {
		return nil
	}
	var out []string
	l.ElementsAs(ctx, &out, false)
	return out
}

func (r *apiKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan apiKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := apiKeyCreateBody{
		Name:             plan.Name.ValueString(),
		BudgetMicros:     int64Ptr(plan.BudgetMicros),
		AllowedModels:    listOrNil(ctx, plan.AllowedModels),
		AllowedProviders: listOrNil(ctx, plan.AllowedProviders),
		RateLimitRPM:     int64Ptr(plan.RateLimitRPM),
		CostCenterID:     strPtr(plan.CostCenterID),
	}
	var out apiKeyCreateResponse
	if err := r.client.do(ctx, "POST", "/api/v1/admin/keys", nil, body, &out); err != nil {
		resp.Diagnostics.AddError("Create API key failed", err.Error())
		return
	}
	plan.ID = types.StringValue(out.ID)
	plan.Key = types.StringValue(out.RawKey)
	plan.KeyPrefix = types.StringValue(out.KeyPrefix)
	plan.Status = types.StringValue(out.Status)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *apiKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state apiKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var list []apiKeyListEntry
	if err := r.client.do(ctx, "GET", "/api/v1/admin/keys", nil, nil, &list); err != nil {
		resp.Diagnostics.AddError("Read API key failed", err.Error())
		return
	}
	id := state.ID.ValueString()
	for i := range list {
		k := &list[i]
		if k.ID != id {
			continue
		}
		if k.Status == "revoked" {
			resp.State.RemoveResource(ctx)
			return
		}
		state.Name = types.StringValue(k.Name)
		state.KeyPrefix = types.StringValue(k.KeyPrefix)
		state.Status = types.StringValue(k.Status)
		if k.BudgetMicros != nil {
			state.BudgetMicros = types.Int64Value(*k.BudgetMicros)
		}
		if k.RateLimitRPM != nil {
			state.RateLimitRPM = types.Int64Value(*k.RateLimitRPM)
		}
		if len(k.AllowedModels) > 0 {
			state.AllowedModels = strList(ctx, &resp.Diagnostics, k.AllowedModels)
		}
		if len(k.AllowedProviders) > 0 {
			state.AllowedProviders = strList(ctx, &resp.Diagnostics, k.AllowedProviders)
		}
		// Only reflect a server-side cost center when present; when absent leave
		// the configured value untouched to avoid a perpetual diff.
		if k.BudgetID != nil && *k.BudgetID != "" {
			state.CostCenterID = types.StringValue(*k.BudgetID)
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
		return
	}
	resp.State.RemoveResource(ctx)
}

type apiKeyUpdateBody struct {
	Name             *string  `json:"name,omitempty"`
	BudgetMicros     *int64   `json:"budgetMicrodollars,omitempty"`
	AllowedModels    []string `json:"allowedModels,omitempty"`
	AllowedProviders []string `json:"allowedProviders,omitempty"`
	RateLimitRPM     *int64   `json:"rateLimitRpm,omitempty"`
	CostCenterID     *string  `json:"costCenterId,omitempty"`
}

func (r *apiKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state apiKeyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := plan.Name.ValueString()
	body := apiKeyUpdateBody{
		Name:             &name,
		BudgetMicros:     int64Ptr(plan.BudgetMicros),
		AllowedModels:    listOrNil(ctx, plan.AllowedModels),
		AllowedProviders: listOrNil(ctx, plan.AllowedProviders),
		RateLimitRPM:     int64Ptr(plan.RateLimitRPM),
		CostCenterID:     strPtr(plan.CostCenterID),
	}
	if err := r.client.do(ctx, "PATCH", "/api/v1/admin/keys/"+state.ID.ValueString(), nil, body, nil); err != nil {
		resp.Diagnostics.AddError("Update API key failed", err.Error())
		return
	}
	// Preserve the create-time plaintext (not returned by PATCH).
	plan.ID = state.ID
	plan.Key = state.Key
	plan.KeyPrefix = state.KeyPrefix
	plan.Status = state.Status
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *apiKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state apiKeyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.do(ctx, "DELETE", "/api/v1/admin/keys/"+state.ID.ValueString(), nil, nil, nil); err != nil && !isNotFound(err) {
		resp.Diagnostics.AddError("Delete API key failed", err.Error())
	}
}

func (r *apiKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("key"), types.StringNull())...)
	resp.Diagnostics.AddWarning(
		"Plaintext not recoverable on import",
		"The gateway does not store the plaintext key. .key is null after import; recreate the key if a downstream output needs it.",
	)
}

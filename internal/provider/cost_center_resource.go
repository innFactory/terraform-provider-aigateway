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
	_ resource.Resource                = (*costCenterResource)(nil)
	_ resource.ResourceWithConfigure   = (*costCenterResource)(nil)
	_ resource.ResourceWithImportState = (*costCenterResource)(nil)
)

type costCenterResource struct {
	client *httpClient
}

func newCostCenterResource() resource.Resource {
	return &costCenterResource{}
}

type costCenterResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Currency    types.String `tfsdk:"currency"`
	Description types.String `tfsdk:"description"`
	IsOrg       types.Bool   `tfsdk:"is_org"`
	MonthlyCap  types.String `tfsdk:"monthly_cap"`
}

func (r *costCenterResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cost_center"
}

func (r *costCenterResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "A cost center (budget) on the gateway. Used to attribute API key usage and, optionally, cap monthly spend.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Server-assigned cost center (budget) uuid.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Cost center (budget) name, unique per tenant.",
			},
			"currency": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "ISO 4217 currency code. Defaults to USD.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Free-form description.",
			},
			"is_org": schema.BoolAttribute{
				Optional:    true,
				Description: "Mark as the org-level cost center.",
			},
			"monthly_cap": schema.StringAttribute{
				Optional:    true,
				Description: "Monthly cap as a decimal string (e.g. \"500.00\"); omit for unlimited (attribution-only).",
			},
		},
	}
}

func (r *costCenterResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*httpClient)
}

type costCenterCreateBody struct {
	Name        string  `json:"name"`
	Currency    string  `json:"currency"`
	Description *string `json:"description,omitempty"`
	IsOrg       *bool   `json:"isOrg,omitempty"`
	MonthlyCap  *string `json:"monthlyCap,omitempty"`
}

type costCenterUpdateBody struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	MonthlyCap  *string `json:"monthlyCap,omitempty"`
}

type costCenterAPI struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Currency    string  `json:"currency"`
	Description string  `json:"description"`
	MonthlyCap  *string `json:"monthlyCap"`
	IsOrg       bool    `json:"isOrg"`
}

func (r *costCenterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan costCenterResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	currency := optString(plan.Currency)
	if currency == "" {
		currency = "USD"
	}
	body := costCenterCreateBody{
		Name:        plan.Name.ValueString(),
		Currency:    currency,
		Description: ptrIf(plan.Description),
		IsOrg:       boolPtr(plan.IsOrg),
		MonthlyCap:  ptrIf(plan.MonthlyCap),
	}
	var out costCenterAPI
	if err := r.client.do(ctx, "POST", "/api/v1/admin/budgets", nil, body, &out); err != nil {
		resp.Diagnostics.AddError("Create cost center failed", err.Error())
		return
	}
	r.apply(&plan, &out, currency)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *costCenterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state costCenterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out costCenterAPI
	if err := r.client.do(ctx, "GET", "/api/v1/admin/budgets/"+state.ID.ValueString(), nil, nil, &out); err != nil {
		if isNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read cost center failed", err.Error())
		return
	}
	r.apply(&state, &out, optString(state.Currency))
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *costCenterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state costCenterResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := costCenterUpdateBody{
		Name:        ptrIf(plan.Name),
		Description: ptrIf(plan.Description),
		MonthlyCap:  ptrIf(plan.MonthlyCap),
	}
	var out costCenterAPI
	if err := r.client.do(ctx, "PATCH", "/api/v1/admin/budgets/"+state.ID.ValueString(), nil, body, &out); err != nil {
		resp.Diagnostics.AddError("Update cost center failed", err.Error())
		return
	}
	plan.ID = state.ID
	r.apply(&plan, &out, optString(plan.Currency))
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *costCenterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state costCenterResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.do(ctx, "DELETE", "/api/v1/admin/budgets/"+state.ID.ValueString(), nil, nil, nil); err != nil && !isNotFound(err) {
		resp.Diagnostics.AddError("Delete cost center failed", err.Error())
	}
}

func (r *costCenterResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// apply copies API fields into the model. currencyFallback resolves the
// Optional+Computed currency to a known value when the gateway omits it.
func (r *costCenterResource) apply(m *costCenterResourceModel, a *costCenterAPI, currencyFallback string) {
	m.ID = types.StringValue(a.ID)
	m.Name = types.StringValue(a.Name)
	if a.Currency != "" {
		m.Currency = types.StringValue(a.Currency)
	} else if currencyFallback != "" {
		m.Currency = types.StringValue(currencyFallback)
	} else {
		m.Currency = types.StringValue("USD")
	}
	// description is Optional (not Computed): only reflect a server value when the
	// response carries one, otherwise keep the planned/null value.
	if a.Description != "" {
		m.Description = types.StringValue(a.Description)
	}
	// is_org is Optional-only (NOT Computed): when unset in config its planned
	// value is null. Reflecting the server's false here would make the result
	// inconsistent with the plan, so only reflect a server value when the model
	// already carries a known (non-null) one. When the config explicitly set it,
	// the gateway echoes it back and we round-trip exactly.
	if !m.IsOrg.IsNull() && !m.IsOrg.IsUnknown() {
		m.IsOrg = types.BoolValue(a.IsOrg)
	}
	// monthly_cap is Optional: leave the configured value untouched when the
	// gateway returns none, reflect it when present.
	if a.MonthlyCap != nil && *a.MonthlyCap != "" {
		m.MonthlyCap = types.StringValue(*a.MonthlyCap)
	}
}

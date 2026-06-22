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
	ID              types.String    `tfsdk:"id"`
	Name            types.String    `tfsdk:"name"`
	Currency        types.String    `tfsdk:"currency"`
	Description     types.String    `tfsdk:"description"`
	IsOrg           types.Bool      `tfsdk:"is_org"`
	Mode            types.String    `tfsdk:"mode"`
	MonthlyCap      types.String    `tfsdk:"monthly_cap"`
	WeeklyCap       types.String    `tfsdk:"weekly_cap"`
	DailyCap        types.String    `tfsdk:"daily_cap"`
	AutoAddNewUsers types.Bool      `tfsdk:"auto_add_new_users"`
	AgentID         types.String    `tfsdk:"agent_id"`
	FallbackChain   types.List      `tfsdk:"fallback_chain"`
	SubLimits       []subLimitModel `tfsdk:"sub_limits"`
}

type subLimitModel struct {
	ScopeType types.String `tfsdk:"scope_type"`
	ScopeID   types.String `tfsdk:"scope_id"`
	AliasName types.String `tfsdk:"alias_name"`
	CapAmount types.String `tfsdk:"cap_amount"`
	DailyCap  types.String `tfsdk:"daily_cap"`
	WeeklyCap types.String `tfsdk:"weekly_cap"`
}

// scopeKey is the stable identity for matching desired vs current sub-limits.
// Alias scopes key on alias_name; the others key on scope_id.
func (s *subLimitModel) scopeKey() string {
	st := optString(s.ScopeType)
	if st == "alias" {
		return "alias:" + optString(s.AliasName)
	}
	return st + ":" + optString(s.ScopeID)
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
			"mode": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Budget mode: pool (one shared counter) | per_user (a counter per member). Defaults to pool.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"monthly_cap": schema.StringAttribute{
				Optional:    true,
				Description: "Monthly cap as a decimal string (e.g. \"500.00\"); omit for unlimited (attribution-only). On update, leaving this unset in config sends null, which CLEARS any cap set outside Terraform (Terraform owns the cap).",
			},
			"weekly_cap": schema.StringAttribute{
				Optional:    true,
				Description: "Weekly cap as a decimal string. \"0\" blocks, a positive number caps, omit/null = unlimited (clearable). Must satisfy daily <= weekly <= monthly when set. On update, leaving this unset in config sends null, which CLEARS any cap set outside Terraform (Terraform owns the cap).",
			},
			"daily_cap": schema.StringAttribute{
				Optional:    true,
				Description: "Daily cap as a decimal string. \"0\" blocks, a positive number caps, omit/null = unlimited (clearable). On update, leaving this unset in config sends null, which CLEARS any cap set outside Terraform (Terraform owns the cap).",
			},
			"auto_add_new_users": schema.BoolAttribute{
				Optional:    true,
				Description: "When true, users provisioned after this budget is created are auto-added as members.",
			},
			"agent_id": schema.StringAttribute{
				Optional:    true,
				Description: "Optional agent id this budget is scoped to.",
			},
			"fallback_chain": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Ordered list of cost center (budget) ids to fall back to when this center is exhausted.",
			},
			"sub_limits": schema.ListNestedAttribute{
				Optional:    true,
				Description: "Fine-grained caps within this budget, scoped to a provider/model/alias/router. Server sub-limits are reflected into state on Read, so out-of-band changes surface as drift in `terraform plan` and are self-healed (reverted to the Terraform-desired config) on the next apply.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"scope_type": schema.StringAttribute{
							Required:    true,
							Description: "Scope dimension: provider | model | alias | router.",
						},
						"scope_id": schema.StringAttribute{
							Optional:    true,
							Description: "The provider id / model id / router id for provider|model|router scopes. Leave null for alias scope.",
						},
						"alias_name": schema.StringAttribute{
							Optional:    true,
							Description: "The alias name for alias scope. Leave null for the other scopes.",
						},
						"cap_amount": schema.StringAttribute{
							Required:    true,
							Description: "Monthly cap for this sub-limit as a decimal string. \"0\" blocks; must be <= the budget monthly_cap when that is set.",
						},
						"daily_cap": schema.StringAttribute{
							Optional:    true,
							Description: "Optional daily cap for this sub-limit (decimal string). \"0\" blocks; null = unlimited.",
						},
						"weekly_cap": schema.StringAttribute{
							Optional:    true,
							Description: "Optional weekly cap for this sub-limit (decimal string). \"0\" blocks; null = unlimited.",
						},
					},
				},
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
	Name            string   `json:"name"`
	Currency        string   `json:"currency"`
	Description     *string  `json:"description,omitempty"`
	IsOrg           *bool    `json:"isOrg,omitempty"`
	Mode            *string  `json:"mode,omitempty"`
	MonthlyCap      *string  `json:"monthlyCap,omitempty"`
	WeeklyCap       *string  `json:"weeklyCap,omitempty"`
	DailyCap        *string  `json:"dailyCap,omitempty"`
	AgentID         *string  `json:"agentId,omitempty"`
	AutoAddNewUsers *bool    `json:"autoAddNewUsers,omitempty"`
	FallbackChain   []string `json:"fallbackChain,omitempty"`
}

// costCenterUpdateBody: caps are NON-omitempty pointers so a nil pointer
// serialises as explicit JSON null → the gateway double-option path CLEARS the
// cap (back to unlimited). A non-nil pointer sets it. Name/description keep
// omitempty (they are not clearable here). fallbackChain/autoAddNewUsers are
// sent when present.
type costCenterUpdateBody struct {
	Name            *string  `json:"name,omitempty"`
	Description     *string  `json:"description,omitempty"`
	MonthlyCap      *string  `json:"monthlyCap"`
	WeeklyCap       *string  `json:"weeklyCap"`
	DailyCap        *string  `json:"dailyCap"`
	AutoAddNewUsers *bool    `json:"autoAddNewUsers,omitempty"`
	FallbackChain   []string `json:"fallbackChain,omitempty"`
}

type costCenterAPI struct {
	ID              string        `json:"id"`
	Name            string        `json:"name"`
	Currency        string        `json:"currency"`
	Description     string        `json:"description"`
	Mode            string        `json:"mode"`
	MonthlyCap      *string       `json:"monthlyCap"`
	WeeklyCap       *string       `json:"weeklyCap"`
	DailyCap        *string       `json:"dailyCap"`
	AgentID         *string       `json:"agentId"`
	AutoAddNewUsers bool          `json:"autoAddNewUsers"`
	FallbackChain   []string      `json:"fallbackChain"`
	IsOrg           bool          `json:"isOrg"`
	SubLimits       []subLimitAPI `json:"subLimits"`
}

type subLimitScopeBody struct {
	Type       string `json:"type"`
	ProviderID string `json:"providerId,omitempty"`
	ModelID    string `json:"modelId,omitempty"`
	AliasName  string `json:"aliasName,omitempty"`
	RouterID   string `json:"routerId,omitempty"`
}

type subLimitCreateBody struct {
	Scope     subLimitScopeBody `json:"scope"`
	CapAmount string            `json:"capAmount"`
	DailyCap  *string           `json:"dailyCap,omitempty"`
	WeeklyCap *string           `json:"weeklyCap,omitempty"`
}

// subLimitUpdateBody sends caps NON-omitempty so a nil pointer clears (explicit null),
// matching the budget-cap clear semantics. capAmount is required (never cleared).
type subLimitUpdateBody struct {
	CapAmount *string `json:"capAmount,omitempty"`
	DailyCap  *string `json:"dailyCap"`
	WeeklyCap *string `json:"weeklyCap"`
}

type subLimitScopeAPI struct {
	Type       string `json:"type"`
	ProviderID string `json:"providerId"`
	ModelID    string `json:"modelId"`
	AliasName  string `json:"aliasName"`
	RouterID   string `json:"routerId"`
}

type subLimitAPI struct {
	ID        string           `json:"id"`
	Scope     subLimitScopeAPI `json:"scope"`
	CapAmount string           `json:"capAmount"`
	DailyCap  *string          `json:"dailyCap"`
	WeeklyCap *string          `json:"weeklyCap"`
}

// toScopeBody maps the flat model scope fields onto the tagged wire scope.
func (s *subLimitModel) toScopeBody() subLimitScopeBody {
	b := subLimitScopeBody{Type: optString(s.ScopeType)}
	switch b.Type {
	case "provider":
		b.ProviderID = optString(s.ScopeID)
	case "model":
		b.ModelID = optString(s.ScopeID)
	case "router":
		b.RouterID = optString(s.ScopeID)
	case "alias":
		b.AliasName = optString(s.AliasName)
	}
	return b
}

// scopeKeyFromAPI mirrors subLimitModel.scopeKey for a server sub-limit.
func scopeKeyFromAPI(a *subLimitAPI) string {
	switch a.Scope.Type {
	case "alias":
		return "alias:" + a.Scope.AliasName
	case "model":
		return "model:" + a.Scope.ModelID
	case "router":
		return "router:" + a.Scope.RouterID
	default: // provider
		return "provider:" + a.Scope.ProviderID
	}
}

// reconcileSubLimits brings the budget's sub-limits to the desired set using
// the gateway sub-resource endpoints: GET current, then create new ones,
// update changed ones, and delete ones no longer desired. Matching is by
// scopeKey (stable scope identity), independent of server-assigned ids.
func (r *costCenterResource) reconcileSubLimits(ctx context.Context, budgetID string, desired []subLimitModel) error {
	base := "/api/v1/admin/budgets/" + budgetID + "/sub-limits"

	var current []subLimitAPI
	if err := r.client.do(ctx, "GET", base, nil, nil, &current); err != nil {
		// Some gateways return the sub-limits only on the budget detail GET; if
		// the dedicated list 404s, treat as empty (create-all path).
		if !isNotFound(err) {
			return err
		}
	}
	byKey := make(map[string]*subLimitAPI, len(current))
	for i := range current {
		byKey[scopeKeyFromAPI(&current[i])] = &current[i]
	}

	desiredKeys := make(map[string]struct{}, len(desired))
	for i := range desired {
		d := &desired[i]
		key := d.scopeKey()
		desiredKeys[key] = struct{}{}
		cap := optString(d.CapAmount)
		if existing, ok := byKey[key]; ok {
			body := subLimitUpdateBody{
				CapAmount: &cap,
				DailyCap:  ptrIf(d.DailyCap),
				WeeklyCap: ptrIf(d.WeeklyCap),
			}
			if err := r.client.do(ctx, "PATCH", base+"/"+existing.ID, nil, body, nil); err != nil {
				return err
			}
		} else {
			body := subLimitCreateBody{
				Scope:     d.toScopeBody(),
				CapAmount: cap,
				DailyCap:  ptrIf(d.DailyCap),
				WeeklyCap: ptrIf(d.WeeklyCap),
			}
			if err := r.client.do(ctx, "POST", base, nil, body, nil); err != nil {
				return err
			}
		}
	}
	// Delete any current sub-limit no longer desired.
	for i := range current {
		if _, ok := desiredKeys[scopeKeyFromAPI(&current[i])]; !ok {
			if err := r.client.do(ctx, "DELETE", base+"/"+current[i].ID, nil, nil, nil); err != nil && !isNotFound(err) {
				return err
			}
		}
	}
	return nil
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
		Name:            plan.Name.ValueString(),
		Currency:        currency,
		Description:     ptrIf(plan.Description),
		IsOrg:           boolPtr(plan.IsOrg),
		Mode:            ptrIf(plan.Mode),
		MonthlyCap:      ptrIf(plan.MonthlyCap),
		WeeklyCap:       ptrIf(plan.WeeklyCap),
		DailyCap:        ptrIf(plan.DailyCap),
		AgentID:         ptrIf(plan.AgentID),
		AutoAddNewUsers: boolPtr(plan.AutoAddNewUsers),
		FallbackChain:   listOrNil(ctx, plan.FallbackChain),
	}
	var out costCenterAPI
	if err := r.client.do(ctx, "POST", "/api/v1/admin/budgets", nil, body, &out); err != nil {
		resp.Diagnostics.AddError("Create cost center failed", err.Error())
		return
	}
	r.apply(&plan, &out, currency)
	if len(plan.SubLimits) > 0 {
		if err := r.reconcileSubLimits(ctx, out.ID, plan.SubLimits); err != nil {
			resp.Diagnostics.AddError("Reconcile cost center sub-limits failed", err.Error())
			return
		}
	}
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
	// Reflect server sub-limits into state so out-of-band changes surface as
	// drift in `terraform plan`. reconcileSubLimits on apply will self-heal any
	// drift back to the desired configuration.
	if out.SubLimits != nil {
		state.SubLimits = subLimitsFromAPI(out.SubLimits)
	}
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
		Name:            ptrIf(plan.Name),
		Description:     ptrIf(plan.Description),
		MonthlyCap:      ptrIf(plan.MonthlyCap),
		WeeklyCap:       ptrIf(plan.WeeklyCap),
		DailyCap:        ptrIf(plan.DailyCap),
		AutoAddNewUsers: boolPtr(plan.AutoAddNewUsers),
		FallbackChain:   listOrNil(ctx, plan.FallbackChain),
	}
	var out costCenterAPI
	if err := r.client.do(ctx, "PATCH", "/api/v1/admin/budgets/"+state.ID.ValueString(), nil, body, &out); err != nil {
		resp.Diagnostics.AddError("Update cost center failed", err.Error())
		return
	}
	plan.ID = state.ID
	r.apply(&plan, &out, optString(plan.Currency))
	if err := r.reconcileSubLimits(ctx, state.ID.ValueString(), plan.SubLimits); err != nil {
		resp.Diagnostics.AddError("Reconcile cost center sub-limits failed", err.Error())
		return
	}
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

// subLimitsFromAPI converts the server sub-limit list to the model slice used
// for state. The mapping is scope-driven: alias scopes populate alias_name;
// all others populate scope_id from their respective typed field.
func subLimitsFromAPI(apiList []subLimitAPI) []subLimitModel {
	out := make([]subLimitModel, 0, len(apiList))
	for _, a := range apiList {
		m := subLimitModel{
			ScopeType: types.StringValue(a.Scope.Type),
			CapAmount: types.StringValue(a.CapAmount),
		}
		switch a.Scope.Type {
		case "alias":
			m.AliasName = types.StringValue(a.Scope.AliasName)
		case "model":
			m.ScopeID = types.StringValue(a.Scope.ModelID)
		case "router":
			m.ScopeID = types.StringValue(a.Scope.RouterID)
		default: // provider
			m.ScopeID = types.StringValue(a.Scope.ProviderID)
		}
		if a.DailyCap != nil {
			m.DailyCap = types.StringValue(*a.DailyCap)
		}
		if a.WeeklyCap != nil {
			m.WeeklyCap = types.StringValue(*a.WeeklyCap)
		}
		out = append(out, m)
	}
	return out
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
	// mode is Optional+Computed (server defaults to "pool"): always reflect.
	if a.Mode != "" {
		m.Mode = types.StringValue(a.Mode)
	}
	// Caps are Optional (not Computed): only reflect a server value when present,
	// leave the planned/null value otherwise (mirrors the existing monthly_cap handling).
	if a.MonthlyCap != nil && *a.MonthlyCap != "" {
		m.MonthlyCap = types.StringValue(*a.MonthlyCap)
	}
	if a.WeeklyCap != nil && *a.WeeklyCap != "" {
		m.WeeklyCap = types.StringValue(*a.WeeklyCap)
	}
	if a.DailyCap != nil && *a.DailyCap != "" {
		m.DailyCap = types.StringValue(*a.DailyCap)
	}
	if a.AgentID != nil && *a.AgentID != "" {
		m.AgentID = types.StringValue(*a.AgentID)
	}
	// auto_add_new_users is Optional-only: only reflect when the model already
	// carries a known value, to avoid an inconsistent-result vs a null plan.
	if !m.AutoAddNewUsers.IsNull() && !m.AutoAddNewUsers.IsUnknown() {
		m.AutoAddNewUsers = types.BoolValue(a.AutoAddNewUsers)
	}
	// fallback_chain is Optional-only and NOT reflected here (leave the configured
	// value) — the gateway stores it but reflecting an empty server list onto a
	// null plan would be an inconsistent result.
}

package provider

import (
	"context"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*tenantSettingsResource)(nil)
	_ resource.ResourceWithConfigure   = (*tenantSettingsResource)(nil)
	_ resource.ResourceWithImportState = (*tenantSettingsResource)(nil)
)

type tenantSettingsResource struct {
	client *httpClient
}

func newTenantSettingsResource() resource.Resource {
	return &tenantSettingsResource{}
}

// tenantSettingsResource is a singleton mapping onto PATCH /api/v1/admin/tenant.
// It manages the org-wide knobs the gateway exposes for an automation flow:
// the default allowed-model list, the org budget cap (null = unlimited), and
// the optional currency / per-user max / default cost center (last-writer-wins).
type tenantSettingsResourceModel struct {
	ID                         types.String `tfsdk:"id"`
	DefaultAllowedModels       types.List   `tfsdk:"default_allowed_models"`
	OrgBudgetMicros            types.Int64  `tfsdk:"org_budget_limit_microdollars"`
	OrgBudgetUnlimited         types.Bool   `tfsdk:"org_budget_unlimited"`
	Currency                   types.String `tfsdk:"currency"`
	DefaultUserBudgetMicros    types.Int64  `tfsdk:"default_user_budget_microdollars"`
	DefaultUserBudgetUnlimited types.Bool   `tfsdk:"default_user_budget_unlimited"`
	DefaultCostCenterID        types.String `tfsdk:"default_cost_center_id"`
	ManagedRevision            types.String `tfsdk:"managed_revision"`
}

func (r *tenantSettingsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_tenant_settings"
}

func (r *tenantSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Singleton tenant-wide settings: default allowed models and the org budget cap.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Always 'tenant' (singleton).",
			},
			"default_allowed_models": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Models visible to all users / trusted-header (LibreChat) callers.",
			},
			"org_budget_limit_microdollars": schema.Int64Attribute{
				Optional:    true,
				Description: "Org monthly budget cap in microdollars. Ignored when org_budget_unlimited = true.",
			},
			"org_budget_unlimited": schema.BoolAttribute{
				Optional:    true,
				Description: "When true, the org budget is set to unlimited (no cap).",
			},
			"currency": schema.StringAttribute{
				Optional:    true,
				Description: "ISO 4217 tenant currency (e.g. EUR, USD). Last-writer-wins: a dashboard edit is not reverted by a no-op apply.",
			},
			"default_user_budget_microdollars": schema.Int64Attribute{
				Optional:    true,
				Description: "Per-user global monthly cap in microdollars (gate 2). Ignored when default_user_budget_unlimited = true.",
			},
			"default_user_budget_unlimited": schema.BoolAttribute{
				Optional:    true,
				Description: "When true, the per-user global max is unlimited (gateway clears the cap).",
			},
			"default_cost_center_id": schema.StringAttribute{
				Optional:    true,
				Description: "Default cost center (budget id) any unscoped key/token attributes to (gate 3 fallback). Empty = unscoped traffic skips gate 3.",
			},
			"managed_revision": schema.StringAttribute{
				// Computed-only (provider-managed), NOT Optional, and deliberately
				// WITHOUT UseStateForUnknown: write() stamps a fresh time.Now() on
				// every apply, so the value legitimately changes on each update.
				// UseStateForUnknown pinned the prior timestamp in the plan while the
				// apply returned a new one -> "Provider produced inconsistent result
				// after apply". Plain Computed plans it as "known after apply" on any
				// update, so the freshly stamped revision is always accepted.
				Computed:    true,
				Description: "Last-writer-wins revision, stamped by the provider on every apply; the gateway only accepts a write whose revision is newer than the stored one. Provider-managed (read-only).",
			},
		},
	}
}

func (r *tenantSettingsResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*httpClient)
}

// tenantPatchBody always transmits the managed scalar fields. The gateway
// interprets orgBudgetLimitMicrodollars == 0 as "unlimited" (clears the cap);
// a positive value sets the cap. defaultUserBudgetMicrodollars is a
// double-option field: null clears the cap (unlimited); a positive int64 sets
// it. 0 would mean "block all users", so we must NOT use 0 as the clear
// sentinel — use nil (→ JSON null) instead. managedRevision is the
// last-writer-wins arbiter: the gateway only applies this write when the
// revision is >= the stored one.
type tenantPatchBody struct {
	DefaultAllowedModels    []string `json:"defaultAllowedModels"`
	OrgBudgetMicros         int64    `json:"orgBudgetLimitMicrodollars"`
	Currency                string   `json:"currency,omitempty"`
	DefaultUserBudgetMicros *int64   `json:"defaultUserBudgetMicrodollars"`
	DefaultCostCenterID     string   `json:"defaultCostCenterId,omitempty"`
	ManagedRevision         string   `json:"managedRevision,omitempty"`
}

type tenantAPI struct {
	DefaultAllowedModels []string `json:"defaultAllowedModels"`
	OrgBudget            *struct {
		MonthlyLimitMicrodollars *int64 `json:"monthlyLimitMicrodollars"`
	} `json:"orgBudget"`
	Currency                      string  `json:"currency"`
	DefaultUserBudgetMicrodollars *int64  `json:"defaultUserBudgetMicrodollars"`
	DefaultCostCenterID           string  `json:"defaultCostCenterId"`
	ManagedRevision               *string `json:"managedRevision"`
}

func (r *tenantSettingsResource) write(ctx context.Context, plan *tenantSettingsResourceModel, diags *diagSink) {
	body := tenantPatchBody{
		DefaultAllowedModels: listOrNil(ctx, plan.DefaultAllowedModels),
		Currency:             optString(plan.Currency),
		DefaultCostCenterID:  optString(plan.DefaultCostCenterID),
		ManagedRevision:      time.Now().UTC().Format(time.RFC3339),
	}
	if plan.OrgBudgetUnlimited.ValueBool() {
		body.OrgBudgetMicros = 0 // 0 → unlimited (gateway clears the cap)
	} else {
		body.OrgBudgetMicros = plan.OrgBudgetMicros.ValueInt64()
	}
	if plan.DefaultUserBudgetUnlimited.ValueBool() {
		body.DefaultUserBudgetMicros = nil // nil → JSON null → gateway clears the per-user cap
	} else {
		v := plan.DefaultUserBudgetMicros.ValueInt64()
		body.DefaultUserBudgetMicros = &v
	}
	// Persist the revision we stamped so it round-trips into state.
	plan.ManagedRevision = types.StringValue(body.ManagedRevision)
	if err := r.client.do(ctx, "PATCH", "/api/v1/admin/tenant", nil, body, nil); err != nil {
		diags.err("Update tenant settings failed", err.Error())
	}
}

// diagSink is a tiny shim so write() can append to either Create/Update diags.
type diagSink struct {
	add func(summary, detail string)
}

func (d *diagSink) err(summary, detail string) { d.add(summary, detail) }

// applyTenantRead reconciles only the fields safe to refresh. The mutable,
// dashboard-editable fields (currency, user-max, default cost center) are
// deliberately NOT copied from the gateway response: last-writer-wins means a
// dashboard edit must not surface as drift and get reverted by the next apply.
// We keep only the org-budget reflection here (it is TF-owned via
// org_budget_unlimited). default_allowed_models is reflected in Read itself,
// where ctx/diags are available for the types.List conversion.
func applyTenantRead(state *tenantSettingsResourceModel, out *tenantAPI) {
	if out.OrgBudget != nil {
		if out.OrgBudget.MonthlyLimitMicrodollars == nil {
			state.OrgBudgetUnlimited = types.BoolValue(true)
		} else {
			state.OrgBudgetMicros = types.Int64Value(*out.OrgBudget.MonthlyLimitMicrodollars)
		}
	}
	// currency / default_user_budget_microdollars / default_cost_center_id:
	// intentionally untouched (last-writer-wins).
}

func (r *tenantSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan tenantSettingsResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.write(ctx, &plan, &diagSink{add: resp.Diagnostics.AddError})
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue("tenant")
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *tenantSettingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state tenantSettingsResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out tenantAPI
	if err := r.client.do(ctx, "GET", "/api/v1/admin/tenant", nil, nil, &out); err != nil {
		resp.Diagnostics.AddError("Read tenant settings failed", err.Error())
		return
	}
	if len(out.DefaultAllowedModels) > 0 {
		state.DefaultAllowedModels = strList(ctx, &resp.Diagnostics, out.DefaultAllowedModels)
	}
	applyTenantRead(&state, &out)
	state.ID = types.StringValue("tenant")
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *tenantSettingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan tenantSettingsResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.write(ctx, &plan, &diagSink{add: resp.Diagnostics.AddError})
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = types.StringValue("tenant")
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete is a no-op: tenant settings are not removable, only reset. We simply
// drop the resource from state.
func (r *tenantSettingsResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

func (r *tenantSettingsResource) ImportState(ctx context.Context, _ resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), "tenant")...)
}

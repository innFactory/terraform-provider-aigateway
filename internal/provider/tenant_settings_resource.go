package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
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
	ID                         types.String  `tfsdk:"id"`
	DefaultAllowedModels       types.List    `tfsdk:"default_allowed_models"`
	OrgBudgetMicros            types.Int64   `tfsdk:"org_budget_limit_microdollars"`
	OrgBudgetUnlimited         types.Bool    `tfsdk:"org_budget_unlimited"`
	Currency                   types.String  `tfsdk:"currency"`
	DefaultUserBudgetMicros    types.Int64   `tfsdk:"default_user_budget_microdollars"`
	DefaultUserBudgetUnlimited types.Bool    `tfsdk:"default_user_budget_unlimited"`
	DefaultCostCenterID        types.String  `tfsdk:"default_cost_center_id"`
	DefaultAccessGroupID       types.String  `tfsdk:"default_access_group_id"`
	AzureCommissionPercent     types.Float64 `tfsdk:"azure_commission_percent"`
	ExternalMarginMicros       types.Int64   `tfsdk:"external_margin_per_1m_tokens_microdollars"`
	OidcProxyDirectBearer      types.String  `tfsdk:"oidc_proxy_direct_bearer"`
	OidcProxyRequiredGroups    types.List    `tfsdk:"oidc_proxy_required_groups"`
	ManagedRevision            types.String  `tfsdk:"managed_revision"`
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
				Description: "Default cost center (budget id) any unscoped key/token attributes to (gate 3 fallback). Removing the attribute after it was set clears the gateway value once (explicit null in the PATCH); while it stays unset it is omitted, so a dashboard-set value survives later applies.",
			},
			"default_access_group_id": schema.StringAttribute{
				Optional:    true,
				Description: "Default access group (aigateway_access_group id) applied to callers in no group — e.g. scopes /v1/models for trusted-header (LibreChat) users. Empty = allow-all when unset. Last-writer-wins: leaving it unset does not clear a dashboard-set value.",
			},
			"azure_commission_percent": schema.Float64Attribute{
				Optional:    true,
				Description: "Reseller commission added on top of Azure provider cost when computing customer_cost (customer_cost = provider_cost × (1 + pct/100)). Set 0 so customer_cost == provider_cost (e.g. internal tenants). Omit to leave the gateway default (20) / a dashboard edit untouched — last-writer-wins.",
			},
			"external_margin_per_1m_tokens_microdollars": schema.Int64Attribute{
				Optional:    true,
				Description: "Flat margin per 1M tokens (microdollars) added to non-Azure provider cost when computing customer_cost (customer_cost = provider_cost + tokens × margin). Set 0 so customer_cost == provider_cost (e.g. internal tenants). Omit to leave the gateway default (25000) / a dashboard edit untouched — last-writer-wins.",
			},
			// Both oidc_proxy_* attributes are Optional ONLY: no Computed, no
			// Default, no plan modifier. Optional+Computed without
			// UseStateForUnknown planned "unknown" on every update and the
			// provider's Update then sent the zero value, silently resetting
			// the setting (the aigateway_provider `enabled` incident). Unset
			// here means "not sent" (last-writer-wins), never "reset".
			"oidc_proxy_direct_bearer": schema.StringAttribute{
				Optional:    true,
				Validators:  []validator.String{oneOfString{"allow", "deny"}},
				Description: "Whether a validated end-user OIDC token may call the proxy (/v1, /mcp) directly as a Bearer. \"deny\" rejects such tokens with 401 direct_bearer_disabled; API keys and the LibreChat trusted-header path are unaffected. Omit to leave the gateway default (allow) / a dashboard edit untouched — last-writer-wins. Requires gateway >= 1.1.4.",
			},
			"oidc_proxy_required_groups": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Entra group ids a direct OIDC bearer must carry at least one of; a token without an authoritative group list is rejected (403 group_required). Set [] to clear. Omit to leave untouched — last-writer-wins. Requires gateway >= 1.1.4.",
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
	DefaultAllowedModels []string `json:"defaultAllowedModels"`
	// Org/user budgets are OMITTED from the PATCH when the tenant leaves them
	// unset (both the *_unlimited flag and the *_microdollars value null) — the
	// gateway then leaves whatever it holds untouched (config.yaml-driven budgets,
	// last-writer-wins). An explicit value or `*_unlimited = true` still writes.
	//   org  : *int64 + omitempty — nil omits; &0 = unlimited; &N = cap.
	//   user : RawMessage + omitempty — nil omits; `null` = unlimited (clear); `N` = cap.
	OrgBudgetMicros         *int64          `json:"orgBudgetLimitMicrodollars,omitempty"`
	Currency                string          `json:"currency,omitempty"`
	DefaultUserBudgetMicros json.RawMessage `json:"defaultUserBudgetMicrodollars,omitempty"`
	// Tri-state like the user budget: nil omits (gateway keeps its value),
	// `null` clears (only on the set → removed transition, see
	// defaultCostCenterPatch), `"id"` sets.
	DefaultCostCenterID  json.RawMessage `json:"defaultCostCenterId,omitempty"`
	DefaultAccessGroupID string          `json:"defaultAccessGroupId,omitempty"`
	// Cost-margin knobs. Pointers with omitempty so an unset attribute is
	// omitted from the PATCH (gateway keeps its default / last-writer-wins),
	// while an explicit 0 is a non-nil pointer and IS sent — the intended way
	// to make customer_cost == provider_cost for internal tenants.
	AzureCommissionPercent *float64 `json:"azureCommissionPercent,omitempty"`
	ExternalMarginMicros   *int64   `json:"externalMarginPer1mTokensMicrodollars,omitempty"`
	// Direct-bearer switch (gateway >= 1.1.4). nil omits the whole object so
	// a dashboard edit survives; inside it only the configured fields travel.
	OidcProxyAuth   *oidcProxyAuthPatch `json:"oidcProxyAuth,omitempty"`
	ManagedRevision string              `json:"managedRevision,omitempty"`
}

// oidcProxyAuthPatch is the PATCH shape of the tenant's oidcProxyAuth object.
// RequiredGroups is a *[]string, not a []string with omitempty: omitempty
// drops an empty slice, but an explicit [] is exactly how the requirement is
// cleared, so it must be sent.
type oidcProxyAuthPatch struct {
	DirectBearer   *string   `json:"directBearer,omitempty"`
	RequiredGroups *[]string `json:"requiredGroups,omitempty"`
}

// oidcProxyAuthAPI is the GET shape; read for completeness, never copied into
// state (last-writer-wins, see applyTenantRead).
type oidcProxyAuthAPI struct {
	DirectBearer   string   `json:"directBearer"`
	RequiredGroups []string `json:"requiredGroups"`
}

type tenantAPI struct {
	DefaultAllowedModels []string `json:"defaultAllowedModels"`
	OrgBudget            *struct {
		MonthlyLimitMicrodollars *int64 `json:"monthlyLimitMicrodollars"`
	} `json:"orgBudget"`
	Currency                      string            `json:"currency"`
	DefaultUserBudgetMicrodollars *int64            `json:"defaultUserBudgetMicrodollars"`
	DefaultCostCenterID           string            `json:"defaultCostCenterId"`
	DefaultAccessGroupID          string            `json:"defaultAccessGroupId"`
	ManagedRevision               *string           `json:"managedRevision"`
	OidcProxyAuth                 *oidcProxyAuthAPI `json:"oidcProxyAuth"`
}

// defaultCostCenterPatch is the defaultCostCenterId value for the PATCH:
//   - configured → the id (set);
//   - removed from config while the prior state still held one → JSON null,
//     which the gateway's double-option field treats as "clear" (it then falls
//     back to its own default attribution);
//   - unset before and after → nil (omitted), so a dashboard-set value is not
//     reverted by an unrelated apply (last-writer-wins).
//
// prior is nil on Create. Clearing on the transition only (not on every apply
// while unset) is what keeps a later dashboard edit from being wiped.
func defaultCostCenterPatch(plan types.String, prior *types.String) json.RawMessage {
	if id := optString(plan); id != "" {
		raw, _ := json.Marshal(id)
		return raw
	}
	if prior != nil && optString(*prior) != "" {
		return json.RawMessage("null")
	}
	return nil
}

func (r *tenantSettingsResource) write(ctx context.Context, plan, prior *tenantSettingsResourceModel, diags *diagSink) {
	var priorCostCenter *types.String
	if prior != nil {
		priorCostCenter = &prior.DefaultCostCenterID
	}
	body := tenantPatchBody{
		DefaultAllowedModels: listOrNil(ctx, plan.DefaultAllowedModels),
		Currency:             optString(plan.Currency),
		DefaultCostCenterID:  defaultCostCenterPatch(plan.DefaultCostCenterID, priorCostCenter),
		DefaultAccessGroupID: optString(plan.DefaultAccessGroupID),
		ManagedRevision:      time.Now().UTC().Format(time.RFC3339),
	}
	// Org budget: send only when the tenant manages it (unlimited flag set, or a
	// cap value set). Both null → omit → gateway keeps its (config-driven) value.
	if !plan.OrgBudgetUnlimited.IsNull() && plan.OrgBudgetUnlimited.ValueBool() {
		z := int64(0) // 0 → unlimited (gateway clears the cap)
		body.OrgBudgetMicros = &z
	} else if !plan.OrgBudgetMicros.IsNull() && !plan.OrgBudgetMicros.IsUnknown() {
		v := plan.OrgBudgetMicros.ValueInt64()
		body.OrgBudgetMicros = &v
	} // else: leave nil → omitempty drops it → no change.
	// Per-user budget: same tri-state via RawMessage (nil omits, `null` clears).
	if !plan.DefaultUserBudgetUnlimited.IsNull() && plan.DefaultUserBudgetUnlimited.ValueBool() {
		body.DefaultUserBudgetMicros = json.RawMessage("null")
	} else if !plan.DefaultUserBudgetMicros.IsNull() && !plan.DefaultUserBudgetMicros.IsUnknown() {
		body.DefaultUserBudgetMicros =
			json.RawMessage(strconv.FormatInt(plan.DefaultUserBudgetMicros.ValueInt64(), 10))
	} // else: leave nil → omitempty drops it → no change.
	// Cost-margin knobs: send only when explicitly configured (0 IS sent).
	if !plan.AzureCommissionPercent.IsNull() && !plan.AzureCommissionPercent.IsUnknown() {
		v := plan.AzureCommissionPercent.ValueFloat64()
		body.AzureCommissionPercent = &v
	}
	if !plan.ExternalMarginMicros.IsNull() && !plan.ExternalMarginMicros.IsUnknown() {
		v := plan.ExternalMarginMicros.ValueInt64()
		body.ExternalMarginMicros = &v
	}
	// Direct-bearer switch: the object is sent only when at least one of the
	// two attributes is configured; each field only when set. An explicitly
	// empty group list is sent as [] (clear), never dropped.
	var oidc *oidcProxyAuthPatch
	if !plan.OidcProxyDirectBearer.IsNull() && !plan.OidcProxyDirectBearer.IsUnknown() {
		v := plan.OidcProxyDirectBearer.ValueString()
		oidc = &oidcProxyAuthPatch{DirectBearer: &v}
	}
	if !plan.OidcProxyRequiredGroups.IsNull() && !plan.OidcProxyRequiredGroups.IsUnknown() {
		var groups []string
		for _, d := range plan.OidcProxyRequiredGroups.ElementsAs(ctx, &groups, false) {
			diags.err(d.Summary(), d.Detail())
		}
		if groups == nil {
			groups = []string{}
		}
		if oidc == nil {
			oidc = &oidcProxyAuthPatch{}
		}
		oidc.RequiredGroups = &groups
	}
	body.OidcProxyAuth = oidc
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
// dashboard- AND config.yaml-editable fields (currency, org/user budgets,
// default cost center) are deliberately NOT copied from the gateway response:
// last-writer-wins means a dashboard or config.yaml edit must not surface as
// drift and get reverted by the next apply. Org/user budgets in particular are
// now config-driven for tenants that leave them unset (the write() side omits
// them), so reflecting the server value here would produce perpetual drift.
// default_allowed_models is reflected in Read itself, where ctx/diags are
// available for the types.List conversion.
func applyTenantRead(_ *tenantSettingsResourceModel, _ *tenantAPI) {
	// org/user budgets, currency, default_cost_center_id, default_access_group_id,
	// oidc_proxy_direct_bearer, oidc_proxy_required_groups:
	// intentionally untouched (last-writer-wins / config-driven).
}

// oneOfString is a plan-time validator for a fixed set of string values. Kept
// in-package so the provider does not pull in terraform-plugin-framework-
// validators for one attribute. Null and unknown values pass (the attribute
// is optional; unknown is resolved at apply).
type oneOfString []string

func (o oneOfString) Description(_ context.Context) string {
	return "value must be one of: " + strings.Join(o, ", ")
}

func (o oneOfString) MarkdownDescription(ctx context.Context) string { return o.Description(ctx) }

func (o oneOfString) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	got := req.ConfigValue.ValueString()
	for _, v := range o {
		if got == v {
			return
		}
	}
	resp.Diagnostics.AddAttributeError(req.Path, "Invalid attribute value",
		fmt.Sprintf("%s: got %q", o.Description(ctx), got))
}

func (r *tenantSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan tenantSettingsResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.write(ctx, &plan, nil, &diagSink{add: resp.Diagnostics.AddError})
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
	var plan, prior tenantSettingsResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.write(ctx, &plan, &prior, &diagSink{add: resp.Diagnostics.AddError})
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

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	aigateway "github.com/innFactory/aigateway-go"
)

var (
	_ resource.Resource                   = (*accessGroupResource)(nil)
	_ resource.ResourceWithConfigure      = (*accessGroupResource)(nil)
	_ resource.ResourceWithImportState    = (*accessGroupResource)(nil)
	_ resource.ResourceWithValidateConfig = (*accessGroupResource)(nil)
)

// validAPIFormats mirrors the gateway's ApiFormat enum (src/models/team.rs),
// serialized lowercase: the inbound wire protocols an access group may use.
var validAPIFormats = []string{"openai", "anthropic", "gemini"}

// accessGroupResource manages a gateway access group (the admin API calls
// them "teams": /api/v1/admin/teams). Access groups bundle model/provider
// visibility, an optional budget, a rate limit and an optional Entra group
// mapping; the tenant default access group (tenant_settings.
// default_access_group_id) scopes what trusted-header (LibreChat) callers see
// on /v1/models.
type accessGroupResource struct {
	client *aigateway.Client
}

func newAccessGroupResource() resource.Resource {
	return &accessGroupResource{}
}

type accessGroupResourceModel struct {
	ID                      types.String `tfsdk:"id"`
	Name                    types.String `tfsdk:"name"`
	Description             types.String `tfsdk:"description"`
	Icon                    types.String `tfsdk:"icon"`
	EntraGroupID            types.String `tfsdk:"entra_group_id"`
	BudgetLimitMicrodollars types.Int64  `tfsdk:"budget_limit_microdollars"`
	RateLimitRpm            types.Int64  `tfsdk:"rate_limit_rpm"`
	AllowedModels           types.List   `tfsdk:"allowed_models"`
	AllowedProviders        types.List   `tfsdk:"allowed_providers"`
	SupportedFormats        types.List   `tfsdk:"supported_formats"`
	Enabled                 types.Bool   `tfsdk:"enabled"`
}

func (r *accessGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_access_group"
}

func (r *accessGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An access group (the gateway admin API calls them teams) — bundles model/provider " +
			"visibility, an optional budget, a rate limit, inbound API-format restrictions and an " +
			"optional Entra group auto-assignment.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Server-assigned access group (team) uuid.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Access group name, unique per tenant.",
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Free-form description. NOTE: the gateway API cannot clear this once set — removing it from config leaves the server value in place (recreate to clear).",
			},
			"icon": schema.StringAttribute{
				Optional:    true,
				Description: "Icon for the group (emoji or icon-name string), rendered by CompanyGPT / LibreChat. Not clearable via the API once set.",
			},
			"entra_group_id": schema.StringAttribute{
				Optional:    true,
				Description: "Entra (Azure AD) group object id mapped to this access group. Users authenticated with this Entra group are auto-assigned. Not clearable via the API once set.",
			},
			"budget_limit_microdollars": schema.Int64Attribute{
				Optional:    true,
				Description: "Monthly budget limit for the group in microdollars. Omit for no group-level budget (only the tenant limit applies). Not clearable via the API once set.",
			},
			"rate_limit_rpm": schema.Int64Attribute{
				Optional:    true,
				Description: "Requests-per-minute rate limit for the group. Omit for no group-level rate limit. Not clearable via the API once set.",
			},
			"allowed_models": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Model ids members of this group may use. Omit = all tenant-enabled models. CAUTION: an empty list restricts to NO models; removing the attribute after it was set leaves the server restriction in place (recreate to clear).",
			},
			"allowed_providers": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Provider ids members of this group may use. Omit = all tenant-enabled providers. CAUTION: an empty list restricts to NO providers; removing the attribute after it was set leaves the server restriction in place (recreate to clear).",
			},
			"supported_formats": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Inbound API formats (wire protocols) this group may use: openai | anthropic | gemini. Omit or empty = no restriction (all formats). Unlike the other restrictions, removing this attribute DOES clear the restriction on the next apply.",
			},
			"enabled": schema.BoolAttribute{
				Optional: true,
				// Computed is required for a schema default; the static default
				// keeps the planned value KNOWN (true) when unset — deliberately
				// NOT bare Optional+Computed, which planned unknown on update and
				// (without UseStateForUnknown) sent false to the gateway,
				// silently disabling the resource (see the managed_revision /
				// enabled incidents, provider v0.4.x).
				Computed:    true,
				Default:     booldefault.StaticBool(true),
				Description: "Whether the access group is active. Defaults to true.",
			},
		},
	}
}

func (r *accessGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*aigateway.Client)
}

// ValidateConfig checks supported_formats against the gateway's ApiFormat
// enum so a typo fails at plan time instead of as a gateway 422.
func (r *accessGroupResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg accessGroupResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	for i, f := range listOrNil(ctx, cfg.SupportedFormats) {
		valid := false
		for _, v := range validAPIFormats {
			if f == v {
				valid = true
				break
			}
		}
		if !valid {
			resp.Diagnostics.AddAttributeError(
				path.Root("supported_formats").AtListIndex(i),
				"Invalid API format",
				fmt.Sprintf("%q is not a gateway API format; must be one of: %s.", f, strings.Join(validAPIFormats, " | ")),
			)
		}
	}
}

func (r *accessGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan accessGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := accessGroupCreateBody{
		Name:                    plan.Name.ValueString(),
		Description:             ptrIf(plan.Description),
		Icon:                    ptrIf(plan.Icon),
		BudgetLimitMicrodollars: int64Ptr(plan.BudgetLimitMicrodollars),
		RateLimitRpm:            int64Ptr(plan.RateLimitRpm),
		AllowedModels:           listOrNil(ctx, plan.AllowedModels),
		AllowedProviders:        listOrNil(ctx, plan.AllowedProviders),
		EntraGroupID:            ptrIf(plan.EntraGroupID),
		SupportedFormats:        listOrNil(ctx, plan.SupportedFormats),
	}
	out, err := r.client.CreateTeam(ctx, body)
	if err != nil {
		resp.Diagnostics.AddError("Create access group failed", err.Error())
		return
	}
	// The create endpoint always creates enabled=true; honour enabled=false in
	// config with a follow-up PATCH.
	if !plan.Enabled.ValueBool() {
		out, err = r.client.SetTeamEnabled(ctx, out.ID, false)
		if err != nil {
			resp.Diagnostics.AddError("Disable access group after create failed", err.Error())
			return
		}
	}
	r.apply(&plan, out, false)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *accessGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state accessGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.client.GetTeam(ctx, state.ID.ValueString())
	if err != nil {
		if isNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read access group failed", err.Error())
		return
	}
	r.apply(&state, out, true)
	// Reflect the server lists on refresh so out-of-band changes surface as
	// drift in `terraform plan` (in Create/Update the planned lists are kept —
	// the gateway echoes what we sent).
	if out.AllowedModels != nil {
		state.AllowedModels = strList(ctx, &resp.Diagnostics, out.AllowedModels)
	} else {
		state.AllowedModels = types.ListNull(types.StringType)
	}
	if out.AllowedProviders != nil {
		state.AllowedProviders = strList(ctx, &resp.Diagnostics, out.AllowedProviders)
	} else {
		state.AllowedProviders = types.ListNull(types.StringType)
	}
	if out.SupportedFormats != nil {
		state.SupportedFormats = strList(ctx, &resp.Diagnostics, out.SupportedFormats)
	} else {
		state.SupportedFormats = types.ListNull(types.StringType)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *accessGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state accessGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	formats := listOrNil(ctx, plan.SupportedFormats)
	if formats == nil {
		formats = []string{} // explicit [] clears the format restriction (gateway: empty vec → None)
	}
	enabled := plan.Enabled.ValueBool()
	body := accessGroupUpdateBody{
		Name:                    ptrIf(plan.Name),
		Description:             ptrIf(plan.Description),
		Icon:                    ptrIf(plan.Icon),
		BudgetLimitMicrodollars: int64Ptr(plan.BudgetLimitMicrodollars),
		RateLimitRpm:            int64Ptr(plan.RateLimitRpm),
		AllowedModels:           listOrNil(ctx, plan.AllowedModels),
		AllowedProviders:        listOrNil(ctx, plan.AllowedProviders),
		EntraGroupID:            ptrIf(plan.EntraGroupID),
		SupportedFormats:        formats,
		Enabled:                 &enabled,
	}
	out, err := r.client.UpdateTeam(ctx, state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Update access group failed", err.Error())
		return
	}
	plan.ID = state.ID
	r.apply(&plan, out, false)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *accessGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state accessGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.DeleteTeam(ctx, state.ID.ValueString()); err != nil && !isNotFound(err) {
		resp.Diagnostics.AddError("Delete access group failed", err.Error())
	}
}

func (r *accessGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// apply copies the scalar API fields into the model.
//
// refresh=true (Read): the server value wins unconditionally — out-of-band
// changes surface as drift in `terraform plan` (including a null when the
// server no longer carries the field).
//
// refresh=false (Create/Update): the Optional scalars are reflected only when
// the plan already carries a known value (the gateway echoes what we sent, so
// this round-trips exactly). A planned null is kept even when the server
// still holds a value — the teams PATCH cannot clear these fields, and
// overwriting the planned null with the server's leftover would be
// "Provider produced inconsistent result after apply".
func (r *accessGroupResource) apply(m *accessGroupResourceModel, a *accessGroupAPI, refresh bool) {
	m.ID = types.StringValue(a.ID)
	m.Name = types.StringValue(a.Name)
	if refresh || (!m.Description.IsNull() && !m.Description.IsUnknown()) {
		m.Description = strPtrOrNull(a.Description)
	}
	if refresh || (!m.Icon.IsNull() && !m.Icon.IsUnknown()) {
		m.Icon = strPtrOrNull(a.Icon)
	}
	if refresh || (!m.EntraGroupID.IsNull() && !m.EntraGroupID.IsUnknown()) {
		m.EntraGroupID = strPtrOrNull(a.EntraGroupID)
	}
	if refresh || (!m.BudgetLimitMicrodollars.IsNull() && !m.BudgetLimitMicrodollars.IsUnknown()) {
		if a.BudgetLimitMicrodollars != nil {
			m.BudgetLimitMicrodollars = types.Int64Value(*a.BudgetLimitMicrodollars)
		} else {
			m.BudgetLimitMicrodollars = types.Int64Null()
		}
	}
	if refresh || (!m.RateLimitRpm.IsNull() && !m.RateLimitRpm.IsUnknown()) {
		if a.RateLimitRpm != nil {
			m.RateLimitRpm = types.Int64Value(*a.RateLimitRpm)
		} else {
			m.RateLimitRpm = types.Int64Null()
		}
	}
	// enabled is Optional+Computed with a static default: the planned value is
	// always known and the gateway echoes the effective flag — always reflect.
	m.Enabled = types.BoolValue(a.Enabled)
}

// strPtrOrNull maps a nil pointer to a null string value.
func strPtrOrNull(p *string) types.String {
	if p == nil {
		return types.StringNull()
	}
	return types.StringValue(*p)
}

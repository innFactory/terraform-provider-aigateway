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
	_ resource.Resource                = (*companygptIntegrationResource)(nil)
	_ resource.ResourceWithConfigure   = (*companygptIntegrationResource)(nil)
	_ resource.ResourceWithImportState = (*companygptIntegrationResource)(nil)
)

type companygptIntegrationResource struct {
	client *httpClient
}

func newCompanygptIntegrationResource() resource.Resource {
	return &companygptIntegrationResource{}
}

// companygptIntegrationResource is a singleton-per-tenant config object mapping
// onto PUT /api/v1/admin/tenant/{tenant_id}/companygpt-integration. It manages
// the companyGPT / LibreChat integration policy: how external (LibreChat) users,
// roles and groups are projected onto gateway users, roles, budgets and teams.
//
// The gateway exposes only the upsert (PUT) endpoint for this policy, so the
// resource is write-mostly: Create/Update both PUT the full policy and store the
// configured values in state; Read is a no-op reconcile (state == last applied
// config); Delete PUTs a disabled, empty policy to deactivate the integration.
type companygptIntegrationResourceModel struct {
	ID                    types.String        `tfsdk:"id"`
	TenantID              types.String        `tfsdk:"tenant_id"`
	Enabled               types.Bool          `tfsdk:"enabled"`
	ExternalTenantIDs     types.List          `tfsdk:"external_tenant_ids"`
	DefaultUserStatus     types.String        `tfsdk:"default_user_status"`
	AllowUnbudgetedUsers  types.Bool          `tfsdk:"allow_unbudgeted_users"`
	DefaultSharedBudgetID types.String        `tfsdk:"default_shared_budget_id"`
	RoleMappings          []roleMappingModel  `tfsdk:"role_mappings"`
	GroupMappings         []groupMappingModel `tfsdk:"group_mappings"`
	ManagedBy             types.String        `tfsdk:"managed_by"`
	ManagedRevision       types.String        `tfsdk:"managed_revision"`
}

type roleMappingModel struct {
	ExternalRole             types.String `tfsdk:"external_role"`
	GatewayRole              types.String `tfsdk:"gateway_role"`
	AllowedModels            types.List   `tfsdk:"allowed_models"`
	UserBudgetMicrodollars   types.Int64  `tfsdk:"user_budget_microdollars"`
	SharedBudgetID           types.String `tfsdk:"shared_budget_id"`
	PerUserLimitMicrodollars types.Int64  `tfsdk:"per_user_limit_microdollars"`
	AllowUnbudgeted          types.Bool   `tfsdk:"allow_unbudgeted"`
}

type groupMappingModel struct {
	ExternalGroupID          types.String `tfsdk:"external_group_id"`
	GatewayRole              types.String `tfsdk:"gateway_role"`
	AllowedModels            types.List   `tfsdk:"allowed_models"`
	TeamID                   types.String `tfsdk:"team_id"`
	UserBudgetMicrodollars   types.Int64  `tfsdk:"user_budget_microdollars"`
	SharedBudgetID           types.String `tfsdk:"shared_budget_id"`
	PerUserLimitMicrodollars types.Int64  `tfsdk:"per_user_limit_microdollars"`
	AllowUnbudgeted          types.Bool   `tfsdk:"allow_unbudgeted"`
}

func (r *companygptIntegrationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_companygpt_integration"
}

func (r *companygptIntegrationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "companyGPT / LibreChat integration policy for a gateway tenant. Singleton per tenant_id; " +
			"maps external (LibreChat) tenants, roles and groups onto gateway users, roles, budgets and teams.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:      true,
				Description:   "Resource id (equals tenant_id).",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"tenant_id": schema.StringAttribute{
				Required:      true,
				Description:   "The gateway tenant this policy applies to (e.g. \"default\"). Immutable.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"enabled": schema.BoolAttribute{
				Required:    true,
				Description: "Whether the companyGPT integration is active for this tenant.",
			},
			"external_tenant_ids": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "External (LibreChat) tenant ids this policy accepts users from.",
			},
			"default_user_status": schema.StringAttribute{
				Optional:    true,
				Description: "Status assigned to newly provisioned users: active | pending | deactivated | blocked.",
			},
			"allow_unbudgeted_users": schema.BoolAttribute{
				Optional:    true,
				Description: "When true, users without a resolved budget are still allowed.",
			},
			"default_shared_budget_id": schema.StringAttribute{
				Optional:    true,
				Description: "Shared budget id applied to users when no mapping overrides it.",
			},
			"managed_by": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Metadata marker for the policy owner. Defaults to \"companygpt-terraform\".",
			},
			"managed_revision": schema.StringAttribute{
				Optional:    true,
				Description: "Optional metadata revision string for the policy.",
			},
			"role_mappings": schema.ListNestedAttribute{
				Optional:    true,
				Description: "Maps external roles onto gateway roles, models and budgets. Note: the gateway rejects admin/owner for role mappings.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"external_role": schema.StringAttribute{
							Required:    true,
							Description: "External (LibreChat) role name to match.",
						},
						"gateway_role": schema.StringAttribute{
							Optional:    true,
							Description: "Gateway role to assign: member | guest (admin/owner are rejected for role mappings).",
						},
						"allowed_models": schema.ListAttribute{
							Optional:    true,
							ElementType: types.StringType,
							Description: "Models allowed for users matched by this role.",
						},
						"user_budget_microdollars": schema.Int64Attribute{
							Optional:    true,
							Description: "Per-user budget in microdollars for this role.",
						},
						"shared_budget_id": schema.StringAttribute{
							Optional:    true,
							Description: "Shared budget id for users matched by this role.",
						},
						"per_user_limit_microdollars": schema.Int64Attribute{
							Optional:    true,
							Description: "Per-user limit within a shared budget, in microdollars.",
						},
						"allow_unbudgeted": schema.BoolAttribute{
							Optional:    true,
							Description: "Allow users matched by this role without a resolved budget.",
						},
					},
				},
			},
			"group_mappings": schema.ListNestedAttribute{
				Optional:    true,
				Description: "Maps external group ids onto gateway roles, teams, models and budgets.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"external_group_id": schema.StringAttribute{
							Required:    true,
							Description: "External (LibreChat) group id to match.",
						},
						"gateway_role": schema.StringAttribute{
							Optional:    true,
							Description: "Gateway role to assign: owner | admin | member | guest.",
						},
						"allowed_models": schema.ListAttribute{
							Optional:    true,
							ElementType: types.StringType,
							Description: "Models allowed for users matched by this group.",
						},
						"team_id": schema.StringAttribute{
							Optional:    true,
							Description: "Gateway team id to assign matched users to.",
						},
						"user_budget_microdollars": schema.Int64Attribute{
							Optional:    true,
							Description: "Per-user budget in microdollars for this group.",
						},
						"shared_budget_id": schema.StringAttribute{
							Optional:    true,
							Description: "Shared budget id for users matched by this group.",
						},
						"per_user_limit_microdollars": schema.Int64Attribute{
							Optional:    true,
							Description: "Per-user limit within a shared budget, in microdollars.",
						},
						"allow_unbudgeted": schema.BoolAttribute{
							Optional:    true,
							Description: "Allow users matched by this group without a resolved budget.",
						},
					},
				},
			},
		},
	}
}

func (r *companygptIntegrationResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*httpClient)
}

// ── wire types (gateway UpsertIntegrationPolicyRequest, camelCase) ─────────────

type roleMappingBody struct {
	ExternalRole             string   `json:"externalRole"`
	GatewayRole              *string  `json:"gatewayRole,omitempty"`
	AllowedModels            []string `json:"allowedModels,omitempty"`
	UserBudgetMicrodollars   *int64   `json:"userBudgetMicrodollars,omitempty"`
	SharedBudgetID           *string  `json:"sharedBudgetId,omitempty"`
	PerUserLimitMicrodollars *int64   `json:"perUserLimitMicrodollars,omitempty"`
	AllowUnbudgeted          *bool    `json:"allowUnbudgeted,omitempty"`
}

type groupMappingBody struct {
	ExternalGroupID          string   `json:"externalGroupId"`
	GatewayRole              *string  `json:"gatewayRole,omitempty"`
	AllowedModels            []string `json:"allowedModels,omitempty"`
	TeamID                   *string  `json:"teamId,omitempty"`
	UserBudgetMicrodollars   *int64   `json:"userBudgetMicrodollars,omitempty"`
	SharedBudgetID           *string  `json:"sharedBudgetId,omitempty"`
	PerUserLimitMicrodollars *int64   `json:"perUserLimitMicrodollars,omitempty"`
	AllowUnbudgeted          *bool    `json:"allowUnbudgeted,omitempty"`
}

type integrationMetadataBody struct {
	ManagedBy       string  `json:"managedBy"`
	ManagedRevision *string `json:"managedRevision,omitempty"`
}

type upsertIntegrationPolicyBody struct {
	Enabled               bool                     `json:"enabled"`
	ExternalTenantIDs     []string                 `json:"externalTenantIds,omitempty"`
	DefaultUserStatus     *string                  `json:"defaultUserStatus,omitempty"`
	AllowUnbudgetedUsers  *bool                    `json:"allowUnbudgetedUsers,omitempty"`
	DefaultSharedBudgetID *string                  `json:"defaultSharedBudgetId,omitempty"`
	RoleMappings          []roleMappingBody        `json:"roleMappings,omitempty"`
	GroupMappings         []groupMappingBody       `json:"groupMappings,omitempty"`
	Metadata              *integrationMetadataBody `json:"metadata,omitempty"`
}

const defaultManagedBy = "companygpt-terraform"

func (m *companygptIntegrationResourceModel) toBody(ctx context.Context) upsertIntegrationPolicyBody {
	b := upsertIntegrationPolicyBody{
		Enabled:               m.Enabled.ValueBool(),
		ExternalTenantIDs:     listOrNil(ctx, m.ExternalTenantIDs),
		DefaultUserStatus:     ptrIf(m.DefaultUserStatus),
		AllowUnbudgetedUsers:  boolPtr(m.AllowUnbudgetedUsers),
		DefaultSharedBudgetID: ptrIf(m.DefaultSharedBudgetID),
		Metadata: &integrationMetadataBody{
			ManagedBy:       defStr(m.ManagedBy, defaultManagedBy),
			ManagedRevision: ptrIf(m.ManagedRevision),
		},
	}
	for i := range m.RoleMappings {
		rm := &m.RoleMappings[i]
		b.RoleMappings = append(b.RoleMappings, roleMappingBody{
			ExternalRole:             rm.ExternalRole.ValueString(),
			GatewayRole:              ptrIf(rm.GatewayRole),
			AllowedModels:            listOrNil(ctx, rm.AllowedModels),
			UserBudgetMicrodollars:   int64Ptr(rm.UserBudgetMicrodollars),
			SharedBudgetID:           ptrIf(rm.SharedBudgetID),
			PerUserLimitMicrodollars: int64Ptr(rm.PerUserLimitMicrodollars),
			AllowUnbudgeted:          boolPtr(rm.AllowUnbudgeted),
		})
	}
	for i := range m.GroupMappings {
		gm := &m.GroupMappings[i]
		b.GroupMappings = append(b.GroupMappings, groupMappingBody{
			ExternalGroupID:          gm.ExternalGroupID.ValueString(),
			GatewayRole:              ptrIf(gm.GatewayRole),
			AllowedModels:            listOrNil(ctx, gm.AllowedModels),
			TeamID:                   ptrIf(gm.TeamID),
			UserBudgetMicrodollars:   int64Ptr(gm.UserBudgetMicrodollars),
			SharedBudgetID:           ptrIf(gm.SharedBudgetID),
			PerUserLimitMicrodollars: int64Ptr(gm.PerUserLimitMicrodollars),
			AllowUnbudgeted:          boolPtr(gm.AllowUnbudgeted),
		})
	}
	return b
}

func (r *companygptIntegrationResource) put(ctx context.Context, m *companygptIntegrationResourceModel) error {
	return r.client.do(ctx, "PUT",
		"/api/v1/admin/tenant/"+m.TenantID.ValueString()+"/companygpt-integration",
		nil, m.toBody(ctx), nil)
}

func (r *companygptIntegrationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan companygptIntegrationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.put(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Set companyGPT integration failed", err.Error())
		return
	}
	plan.ID = plan.TenantID
	plan.ManagedBy = types.StringValue(defStr(plan.ManagedBy, defaultManagedBy))
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Read is a no-op reconcile. The gateway exposes only the upsert (PUT) endpoint
// for the integration policy, so there is nothing to GET; state mirrors the last
// applied configuration. We only keep id/managed_by populated.
func (r *companygptIntegrationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state companygptIntegrationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.ID = state.TenantID
	if state.ManagedBy.IsNull() || state.ManagedBy.IsUnknown() {
		state.ManagedBy = types.StringValue(defaultManagedBy)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *companygptIntegrationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan companygptIntegrationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.put(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Update companyGPT integration failed", err.Error())
		return
	}
	plan.ID = plan.TenantID
	plan.ManagedBy = types.StringValue(defStr(plan.ManagedBy, defaultManagedBy))
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Delete deactivates the integration by PUTting a disabled, empty policy.
func (r *companygptIntegrationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state companygptIntegrationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := upsertIntegrationPolicyBody{
		Enabled:  false,
		Metadata: &integrationMetadataBody{ManagedBy: defaultManagedBy},
	}
	if err := r.client.do(ctx, "PUT",
		"/api/v1/admin/tenant/"+state.TenantID.ValueString()+"/companygpt-integration",
		nil, body, nil); err != nil && !isNotFound(err) {
		resp.Diagnostics.AddError("Disable companyGPT integration failed", err.Error())
	}
}

func (r *companygptIntegrationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("tenant_id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

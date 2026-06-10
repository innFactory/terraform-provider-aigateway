package provider

import (
	"context"

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
// the default allowed-model list and the org budget cap (null = unlimited).
type tenantSettingsResourceModel struct {
	ID                   types.String `tfsdk:"id"`
	DefaultAllowedModels types.List   `tfsdk:"default_allowed_models"`
	OrgBudgetMicros      types.Int64  `tfsdk:"org_budget_limit_microdollars"`
	OrgBudgetUnlimited   types.Bool   `tfsdk:"org_budget_unlimited"`
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
		},
	}
}

func (r *tenantSettingsResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*httpClient)
}

// tenantPatchBody always transmits the managed fields. orgBudgetLimitMicrodollars
// has NO omitempty: when the pointer is nil it serialises as JSON null, which
// the gateway interprets as "unlimited".
type tenantPatchBody struct {
	DefaultAllowedModels []string `json:"defaultAllowedModels"`
	OrgBudgetMicros      *int64   `json:"orgBudgetLimitMicrodollars"`
}

type tenantAPI struct {
	DefaultAllowedModels []string `json:"defaultAllowedModels"`
	OrgBudget            *struct {
		MonthlyLimitMicrodollars *int64 `json:"monthlyLimitMicrodollars"`
	} `json:"orgBudget"`
}

func (r *tenantSettingsResource) write(ctx context.Context, plan *tenantSettingsResourceModel, diags *diagSink) {
	body := tenantPatchBody{
		DefaultAllowedModels: listOrNil(ctx, plan.DefaultAllowedModels),
	}
	if plan.OrgBudgetUnlimited.ValueBool() {
		body.OrgBudgetMicros = nil // → JSON null → unlimited
	} else {
		body.OrgBudgetMicros = int64Ptr(plan.OrgBudgetMicros)
	}
	if err := r.client.do(ctx, "PATCH", "/api/v1/admin/tenant", nil, body, nil); err != nil {
		diags.err("Update tenant settings failed", err.Error())
	}
}

// diagSink is a tiny shim so write() can append to either Create/Update diags.
type diagSink struct {
	add func(summary, detail string)
}

func (d *diagSink) err(summary, detail string) { d.add(summary, detail) }

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
	if out.OrgBudget != nil {
		if out.OrgBudget.MonthlyLimitMicrodollars == nil {
			state.OrgBudgetUnlimited = types.BoolValue(true)
		} else {
			state.OrgBudgetMicros = types.Int64Value(*out.OrgBudget.MonthlyLimitMicrodollars)
		}
	}
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

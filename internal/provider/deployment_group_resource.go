package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*deploymentGroupResource)(nil)
	_ resource.ResourceWithConfigure   = (*deploymentGroupResource)(nil)
	_ resource.ResourceWithImportState = (*deploymentGroupResource)(nil)
)

type deploymentGroupResource struct {
	client *httpClient
}

func newDeploymentGroupResource() resource.Resource {
	return &deploymentGroupResource{}
}

// A deployment group makes one gateway model load-balance across several
// provider deployments (multi-region / multi-provider) with a strategy, retry
// policy and cooldown/health config. Maps to PUT /models/{id}/deployment-group.
type deploymentGroupResourceModel struct {
	ModelID        types.String      `tfsdk:"model_id"`
	Strategy       types.String      `tfsdk:"strategy"`
	Deployments    []deploymentModel `tfsdk:"deployments"`
	RetryPolicy    *retryPolicyModel `tfsdk:"retry_policy"`
	CooldownConfig *cooldownModel    `tfsdk:"cooldown_config"`
}

type deploymentModel struct {
	ProviderID      types.String `tfsdk:"provider_id"`
	ProviderModelID types.String `tfsdk:"provider_model_id"`
	DeploymentName  types.String `tfsdk:"deployment_name"`
	Weight          types.Int64  `tfsdk:"weight"`
	Priority        types.Int64  `tfsdk:"priority"`
	Enabled         types.Bool   `tfsdk:"enabled"`
	TimeoutSeconds  types.Int64  `tfsdk:"timeout_seconds"`
}

type retryPolicyModel struct {
	MaxRetries          types.Int64 `tfsdk:"max_retries"`
	BackoffBaseMs       types.Int64 `tfsdk:"backoff_base_ms"`
	BackoffMaxMs        types.Int64 `tfsdk:"backoff_max_ms"`
	TotalTimeoutSeconds types.Int64 `tfsdk:"total_timeout_seconds"`
}

type cooldownModel struct {
	ConsecutiveErrorThreshold types.Int64 `tfsdk:"consecutive_error_threshold"`
	BaseCooldownSeconds       types.Int64 `tfsdk:"base_cooldown_seconds"`
	MaxCooldownSeconds        types.Int64 `tfsdk:"max_cooldown_seconds"`
	HealthyThresholdPercent   types.Int64 `tfsdk:"healthy_threshold_percent"`
	UnhealthyThresholdPercent types.Int64 `tfsdk:"unhealthy_threshold_percent"`
}

func (r *deploymentGroupResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_deployment_group"
}

func (r *deploymentGroupResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Load-balances one model across multiple provider deployments (multi-region / multi-provider) with a strategy, retry policy and cooldown/health config.",
		Attributes: map[string]schema.Attribute{
			"model_id": schema.StringAttribute{
				Required:      true,
				Description:   "The model this deployment group belongs to.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"strategy": schema.StringAttribute{
				Optional:      true,
				Computed:      true,
				Description:   "Load-balancer strategy: round_robin | weighted_random | latency_based | least_busy. Defaults to round_robin.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"deployments": schema.ListNestedAttribute{
				Required:    true,
				Description: "Provider deployments that serve this model.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"provider_id": schema.StringAttribute{
							Required:    true,
							Description: "ID of the aigateway_provider serving this deployment.",
						},
						"provider_model_id": schema.StringAttribute{
							Required:    true,
							Description: "Model id as the upstream provider knows it.",
						},
						"deployment_name": schema.StringAttribute{
							Optional:    true,
							Description: "Azure deployment name (azure_openai).",
						},
						"weight": schema.Int64Attribute{
							Optional:      true,
							Computed:      true,
							Description:   "Traffic weight (1-100, higher = more traffic). Defaults to 100.",
							PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
						},
						"priority": schema.Int64Attribute{
							Optional:      true,
							Computed:      true,
							Description:   "Priority tier (0 = highest). Lower tiers used only when higher are exhausted.",
							PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
						},
						"enabled": schema.BoolAttribute{
							Optional:      true,
							Computed:      true,
							Description:   "Whether this deployment is enabled. Defaults to true.",
							PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
						},
						"timeout_seconds": schema.Int64Attribute{
							Optional:    true,
							Description: "Per-deployment timeout (overrides the model default).",
						},
					},
				},
			},
			"retry_policy": schema.SingleNestedAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Retry policy for the group.",
				Attributes: map[string]schema.Attribute{
					"max_retries":           schema.Int64Attribute{Optional: true, Computed: true, Description: "Max retry attempts (default 2)."},
					"backoff_base_ms":       schema.Int64Attribute{Optional: true, Computed: true, Description: "Base backoff in ms (default 500)."},
					"backoff_max_ms":        schema.Int64Attribute{Optional: true, Computed: true, Description: "Max backoff in ms (default 4000)."},
					"total_timeout_seconds": schema.Int64Attribute{Optional: true, Computed: true, Description: "Total retry+fallback timeout in s (default 120)."},
				},
			},
			"cooldown_config": schema.SingleNestedAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Cooldown / health config for the group.",
				Attributes: map[string]schema.Attribute{
					"consecutive_error_threshold": schema.Int64Attribute{Optional: true, Computed: true, Description: "Consecutive errors before cooldown (default 3)."},
					"base_cooldown_seconds":       schema.Int64Attribute{Optional: true, Computed: true, Description: "Base cooldown in s (default 60)."},
					"max_cooldown_seconds":        schema.Int64Attribute{Optional: true, Computed: true, Description: "Max cooldown in s (default 3600)."},
					"healthy_threshold_percent":   schema.Int64Attribute{Optional: true, Computed: true, Description: "Success-rate %% above which a deployment is Healthy (default 95)."},
					"unhealthy_threshold_percent": schema.Int64Attribute{Optional: true, Computed: true, Description: "Success-rate %% below which a deployment is Unhealthy (default 50)."},
				},
			},
		},
	}
}

func (r *deploymentGroupResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	r.client = req.ProviderData.(*httpClient)
}

// ── wire types ───────────────────────────────────────────────────────────────

type deploymentBody struct {
	ProviderID      string  `json:"providerId"`
	ProviderModelID string  `json:"providerModelId"`
	DeploymentName  *string `json:"deploymentName,omitempty"`
	Weight          *int64  `json:"weight,omitempty"`
	Priority        *int64  `json:"priority,omitempty"`
	Enabled         *bool   `json:"enabled,omitempty"`
	TimeoutSeconds  *int64  `json:"timeoutSeconds,omitempty"`
}

type retryBody struct {
	MaxRetries          *int64 `json:"maxRetries,omitempty"`
	BackoffBaseMs       *int64 `json:"backoffBaseMs,omitempty"`
	BackoffMaxMs        *int64 `json:"backoffMaxMs,omitempty"`
	TotalTimeoutSeconds *int64 `json:"totalTimeoutSeconds,omitempty"`
}

type cooldownBody struct {
	ConsecutiveErrorThreshold *int64 `json:"consecutiveErrorThreshold,omitempty"`
	BaseCooldownSeconds       *int64 `json:"baseCooldownSeconds,omitempty"`
	MaxCooldownSeconds        *int64 `json:"maxCooldownSeconds,omitempty"`
	HealthyThresholdPercent   *int64 `json:"healthyThresholdPercent,omitempty"`
	UnhealthyThresholdPercent *int64 `json:"unhealthyThresholdPercent,omitempty"`
}

type deploymentGroupBody struct {
	Deployments    []deploymentBody `json:"deployments"`
	Strategy       string           `json:"strategy,omitempty"`
	RetryPolicy    *retryBody       `json:"retryPolicy,omitempty"`
	CooldownConfig *cooldownBody    `json:"cooldownConfig,omitempty"`
}

type deploymentGroupAPI struct {
	Deployments []struct {
		ProviderID      string  `json:"providerId"`
		ProviderModelID string  `json:"providerModelId"`
		DeploymentName  *string `json:"deploymentName"`
		Weight          int64   `json:"weight"`
		Priority        int64   `json:"priority"`
		Enabled         bool    `json:"enabled"`
		TimeoutSeconds  *int64  `json:"timeoutSeconds"`
	} `json:"deployments"`
	Strategy    string `json:"strategy"`
	RetryPolicy struct {
		MaxRetries          int64 `json:"maxRetries"`
		BackoffBaseMs       int64 `json:"backoffBaseMs"`
		BackoffMaxMs        int64 `json:"backoffMaxMs"`
		TotalTimeoutSeconds int64 `json:"totalTimeoutSeconds"`
	} `json:"retryPolicy"`
	CooldownConfig struct {
		ConsecutiveErrorThreshold int64 `json:"consecutiveErrorThreshold"`
		BaseCooldownSeconds       int64 `json:"baseCooldownSeconds"`
		MaxCooldownSeconds        int64 `json:"maxCooldownSeconds"`
		HealthyThresholdPercent   int64 `json:"healthyThresholdPercent"`
		UnhealthyThresholdPercent int64 `json:"unhealthyThresholdPercent"`
	} `json:"cooldownConfig"`
}

func (m *deploymentGroupResourceModel) toBody() deploymentGroupBody {
	b := deploymentGroupBody{Strategy: optString(m.Strategy)}
	for i := range m.Deployments {
		d := &m.Deployments[i]
		b.Deployments = append(b.Deployments, deploymentBody{
			ProviderID:      d.ProviderID.ValueString(),
			ProviderModelID: d.ProviderModelID.ValueString(),
			DeploymentName:  ptrIf(d.DeploymentName),
			Weight:          int64Ptr(d.Weight),
			Priority:        int64Ptr(d.Priority),
			Enabled:         boolPtr(d.Enabled),
			TimeoutSeconds:  int64Ptr(d.TimeoutSeconds),
		})
	}
	if rp := m.RetryPolicy; rp != nil {
		b.RetryPolicy = &retryBody{
			MaxRetries:          int64Ptr(rp.MaxRetries),
			BackoffBaseMs:       int64Ptr(rp.BackoffBaseMs),
			BackoffMaxMs:        int64Ptr(rp.BackoffMaxMs),
			TotalTimeoutSeconds: int64Ptr(rp.TotalTimeoutSeconds),
		}
	}
	if cc := m.CooldownConfig; cc != nil {
		b.CooldownConfig = &cooldownBody{
			ConsecutiveErrorThreshold: int64Ptr(cc.ConsecutiveErrorThreshold),
			BaseCooldownSeconds:       int64Ptr(cc.BaseCooldownSeconds),
			MaxCooldownSeconds:        int64Ptr(cc.MaxCooldownSeconds),
			HealthyThresholdPercent:   int64Ptr(cc.HealthyThresholdPercent),
			UnhealthyThresholdPercent: int64Ptr(cc.UnhealthyThresholdPercent),
		}
	}
	return b
}

// apply copies the API response into the model (so computed defaults land in state).
func (m *deploymentGroupResourceModel) apply(a *deploymentGroupAPI) {
	m.Strategy = types.StringValue(a.Strategy)
	m.Deployments = nil
	for i := range a.Deployments {
		d := &a.Deployments[i]
		dm := deploymentModel{
			ProviderID:      types.StringValue(d.ProviderID),
			ProviderModelID: types.StringValue(d.ProviderModelID),
			DeploymentName:  types.StringNull(),
			Weight:          types.Int64Value(d.Weight),
			Priority:        types.Int64Value(d.Priority),
			Enabled:         types.BoolValue(d.Enabled),
			TimeoutSeconds:  types.Int64Null(),
		}
		if d.DeploymentName != nil {
			dm.DeploymentName = types.StringValue(*d.DeploymentName)
		}
		if d.TimeoutSeconds != nil {
			dm.TimeoutSeconds = types.Int64Value(*d.TimeoutSeconds)
		}
		m.Deployments = append(m.Deployments, dm)
	}
	m.RetryPolicy = &retryPolicyModel{
		MaxRetries:          types.Int64Value(a.RetryPolicy.MaxRetries),
		BackoffBaseMs:       types.Int64Value(a.RetryPolicy.BackoffBaseMs),
		BackoffMaxMs:        types.Int64Value(a.RetryPolicy.BackoffMaxMs),
		TotalTimeoutSeconds: types.Int64Value(a.RetryPolicy.TotalTimeoutSeconds),
	}
	m.CooldownConfig = &cooldownModel{
		ConsecutiveErrorThreshold: types.Int64Value(a.CooldownConfig.ConsecutiveErrorThreshold),
		BaseCooldownSeconds:       types.Int64Value(a.CooldownConfig.BaseCooldownSeconds),
		MaxCooldownSeconds:        types.Int64Value(a.CooldownConfig.MaxCooldownSeconds),
		HealthyThresholdPercent:   types.Int64Value(a.CooldownConfig.HealthyThresholdPercent),
		UnhealthyThresholdPercent: types.Int64Value(a.CooldownConfig.UnhealthyThresholdPercent),
	}
}

func (r *deploymentGroupResource) put(ctx context.Context, m *deploymentGroupResourceModel) (*deploymentGroupAPI, error) {
	var out deploymentGroupAPI
	err := r.client.do(ctx, "PUT", "/api/v1/admin/models/"+m.ModelID.ValueString()+"/deployment-group", nil, m.toBody(), &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *deploymentGroupResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan deploymentGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.put(ctx, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Set deployment group failed", err.Error())
		return
	}
	plan.apply(out)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *deploymentGroupResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state deploymentGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// GET returns the group object, or `null` when the model has no group.
	var out *deploymentGroupAPI
	err := r.client.do(ctx, "GET", "/api/v1/admin/models/"+state.ModelID.ValueString()+"/deployment-group", nil, nil, &out)
	if isNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Read deployment group failed", err.Error())
		return
	}
	if out == nil {
		// Group was cleared out-of-band.
		resp.State.RemoveResource(ctx)
		return
	}
	state.apply(out)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *deploymentGroupResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan deploymentGroupResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.put(ctx, &plan)
	if err != nil {
		resp.Diagnostics.AddError("Update deployment group failed", err.Error())
		return
	}
	plan.apply(out)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *deploymentGroupResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state deploymentGroupResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// An empty deployments list removes the group (reverts to the legacy 1:1 binding).
	empty := deploymentGroupBody{Deployments: []deploymentBody{}}
	if err := r.client.do(ctx, "PUT", "/api/v1/admin/models/"+state.ModelID.ValueString()+"/deployment-group", nil, empty, nil); err != nil && !isNotFound(err) {
		resp.Diagnostics.AddError("Clear deployment group failed", err.Error())
	}
}

func (r *deploymentGroupResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("model_id"), req.ID)...)
}

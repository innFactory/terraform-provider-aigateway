# Terraform Provider — Cost-Center Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the `terraform-provider-aigateway` so `aigateway_cost_center` exposes the full budget model (mode, daily/weekly/monthly caps, auto-add, agent, fallback chain, sub-limits with clearable caps), `aigateway_tenant_settings` exposes currency + per-user max + default cost center with last-writer-wins, and `aigateway_companygpt_integration` group/role mappings carry `allowed_providers`/`allowed_models` — then tag the module `v0.7.0`.

**Architecture:** Additive schema growth on three existing resources following the established v0.6.1 patterns (`httpClient.do`, `ptrIf`/`int64Ptr`/`boolPtr`/`listOrNil`, camelCase JSON wire bodies, `apply()` reflection, `UseStateForUnknown` only on server-defaulted scalars). Caps are **clearable**: their plan modifiers must NOT use `UseStateForUnknown` and their update bodies must NOT use `omitempty` — they serialise an explicit JSON `null` so the gateway's `Option<Option<Decimal>>` (double-option) PATCH path clears them. Sub-limits are reconciled against the gateway's dedicated `/budgets/{id}/sub-limits` sub-resource endpoints (the budget create/update body has no inline `subLimits` field), so the cost-center resource diffs desired-vs-actual sub-limits on every write. Tenant-settings mutable fields use the gateway `managedRevision` mechanism so a no-op apply never reverts a dashboard edit (last-writer-wins).

**Tech Stack:** Go 1.22, `github.com/hashicorp/terraform-plugin-framework`, `encoding/json`, gateway admin REST API (camelCase), `go test` unit tests on body marshalling + `apply()` reflection (acceptance tests gated behind `TF_ACC` are optional/manual — they need a live gateway).

## Global Constraints

- Module path: `github.com/innFactory/terraform-provider-aigateway`. Package under test: `internal/provider`.
- Released version is set by GoReleaser via `-ldflags -X main.version={{.Version}}` on the **git tag**; there is NO in-repo version constant to edit. "Bump to v0.7.0" = tag `v0.7.0` after merge (see Release).
- Caps tri-state everywhere (matches gateway): `"0"` = blocked · positive number = capped · `null` = unlimited. Caps are transmitted as JSON **strings** (decimal-as-string), matching the existing `monthly_cap` field and the gateway's `Decimal` deserialiser which accepts numeric strings.
- **Clearable cap fields** (`daily_cap`, `weekly_cap`, `monthly_cap`, and every `sub_limit` cap): do NOT attach `UseStateForUnknown`; do NOT use `omitempty` in the **update** body — send explicit `null` to clear. The gateway update path reads them via `double_option::deserialize` (absent → keep, `null` → clear, value → set).
- **Non-cap server-defaulted scalars** (`id`, `currency`, `enabled`, `auth_type`, `api_version`, `managed_by`) keep `UseStateForUnknown` — the v0.6.1 fix; do not regress it.
- Wire field names are camelCase and MUST match the gateway exactly (verified against `ai-gateway/src/routes/admin/budgets/mod.rs` and `src/models/budget.rs`): `monthlyCap`, `dailyCap`, `weeklyCap`, `mode` (`"pool"`/`"per_user"`), `agentId`, `autoAddNewUsers`, `fallbackChain` (array of budget-id strings), sub-limit `scope` is an internally-tagged object `{ "type": "provider"|"model"|"alias"|"router", "providerId"|"modelId"|"aliasName"|"routerId": "..." }`, `capAmount`, `dailyCap`, `weeklyCap`, `label`; tenant settings `currency`, `defaultUserBudgetMicrodollars`, `defaultCostCenterId`, `managedRevision`; integration mapping `allowedProviders`, `allowedModels`.
- Run `gofmt -l internal/provider` (must print nothing) and `go test ./internal/provider/...` before each task's commit.
- Conventional-commit messages; one logical change per commit.
- This plan depends on the gateway Phase 1 plan (`ai-gateway/docs/superpowers/plans/2026-06-21-gateway-cost-center-phase1.md`). Two interface gaps in that plan MUST be closed gateway-side before Tasks 2–4 can integration-test (see "Cross-plan gaps" at the bottom); the provider unit tests in this plan do not require a live gateway and can land first.

---

### Task 1: Add `allowed_providers` to integration group + role mappings

**Files:**
- Modify: `internal/provider/companygpt_integration_resource.go` (model structs `roleMappingModel`/`groupMappingModel`, schema nested attributes, wire bodies `roleMappingBody`/`groupMappingBody`, `toBody`)
- Test: `internal/provider/companygpt_integration_test.go`

**Interfaces:**
- Produces: `roleMappingModel.AllowedProviders types.List` (tfsdk `allowed_providers`), `groupMappingModel.AllowedProviders types.List`; wire structs gain `AllowedProviders []string` with json tag `allowedProviders,omitempty`. `allowed_models` already exists on both — no change to it. (Access lists are symmetric across role + group mappings per the design; `allowed_models` is the existing field, `allowed_providers` is the new one.)

- [ ] **Step 1: Write the failing test**

Add to `internal/provider/companygpt_integration_test.go`:

```go
func TestCompanygptIntegrationGroupMappingAllowedProviders(t *testing.T) {
	ctx := context.Background()
	m := companygptIntegrationResourceModel{
		TenantID: types.StringValue("default"),
		Enabled:  types.BoolValue(true),
		GroupMappings: []groupMappingModel{{
			ExternalGroupID:  types.StringValue("grp-no-anthropic"),
			AllowedModels:    strListVal([]string{"gpt-5.4"}),
			AllowedProviders: strListVal([]string{"provider_openai"}),
		}},
		ManagedBy:       types.StringNull(),
		ManagedRevision: types.StringNull(),
	}
	raw, err := json.Marshal(m.toBody(ctx))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"enabled":true,"groupMappings":[{"externalGroupId":"grp-no-anthropic","allowedModels":["gpt-5.4"],"allowedProviders":["provider_openai"]}],"metadata":{"managedBy":"companygpt-terraform"}}`
	if string(raw) != want {
		t.Errorf("toBody()=%s\nwant %s", raw, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/provider/ -run TestCompanygptIntegrationGroupMappingAllowedProviders -v`
Expected: FAIL — compile error `unknown field 'AllowedProviders' in struct literal of type provider.groupMappingModel`.

- [ ] **Step 3: Add the model fields**

In `companygpt_integration_resource.go`, add `AllowedProviders` to both nested models (place it right after `AllowedModels`):

```go
type roleMappingModel struct {
	ExternalRole             types.String `tfsdk:"external_role"`
	GatewayRole              types.String `tfsdk:"gateway_role"`
	AllowedModels            types.List   `tfsdk:"allowed_models"`
	AllowedProviders         types.List   `tfsdk:"allowed_providers"`
	UserBudgetMicrodollars   types.Int64  `tfsdk:"user_budget_microdollars"`
	SharedBudgetID           types.String `tfsdk:"shared_budget_id"`
	PerUserLimitMicrodollars types.Int64  `tfsdk:"per_user_limit_microdollars"`
	AllowUnbudgeted          types.Bool   `tfsdk:"allow_unbudgeted"`
}

type groupMappingModel struct {
	ExternalGroupID          types.String `tfsdk:"external_group_id"`
	GatewayRole              types.String `tfsdk:"gateway_role"`
	AllowedModels            types.List   `tfsdk:"allowed_models"`
	AllowedProviders         types.List   `tfsdk:"allowed_providers"`
	TeamID                   types.String `tfsdk:"team_id"`
	UserBudgetMicrodollars   types.Int64  `tfsdk:"user_budget_microdollars"`
	SharedBudgetID           types.String `tfsdk:"shared_budget_id"`
	PerUserLimitMicrodollars types.Int64  `tfsdk:"per_user_limit_microdollars"`
	AllowUnbudgeted          types.Bool   `tfsdk:"allow_unbudgeted"`
}
```

- [ ] **Step 4: Add the schema attributes**

In `Schema`, inside the `role_mappings` `NestedObject.Attributes` map, after `allowed_models`:

```go
							"allowed_providers": schema.ListAttribute{
								Optional:    true,
								ElementType: types.StringType,
								Description: "Provider ids allowed for users matched by this role. Empty = no provider restriction from this mapping.",
							},
```

In the `group_mappings` `NestedObject.Attributes` map, after `allowed_models`:

```go
							"allowed_providers": schema.ListAttribute{
								Optional:    true,
								ElementType: types.StringType,
								Description: "Provider ids allowed for users matched by this group (e.g. exclude Anthropic). Empty = no provider restriction from this mapping.",
							},
```

- [ ] **Step 5: Add the wire fields**

Add `AllowedProviders` to both wire bodies (after `AllowedModels`):

```go
type roleMappingBody struct {
	ExternalRole             string   `json:"externalRole"`
	GatewayRole              *string  `json:"gatewayRole,omitempty"`
	AllowedModels            []string `json:"allowedModels,omitempty"`
	AllowedProviders         []string `json:"allowedProviders,omitempty"`
	UserBudgetMicrodollars   *int64   `json:"userBudgetMicrodollars,omitempty"`
	SharedBudgetID           *string  `json:"sharedBudgetId,omitempty"`
	PerUserLimitMicrodollars *int64   `json:"perUserLimitMicrodollars,omitempty"`
	AllowUnbudgeted          *bool    `json:"allowUnbudgeted,omitempty"`
}

type groupMappingBody struct {
	ExternalGroupID          string   `json:"externalGroupId"`
	GatewayRole              *string  `json:"gatewayRole,omitempty"`
	AllowedModels            []string `json:"allowedModels,omitempty"`
	AllowedProviders         []string `json:"allowedProviders,omitempty"`
	TeamID                   *string  `json:"teamId,omitempty"`
	UserBudgetMicrodollars   *int64   `json:"userBudgetMicrodollars,omitempty"`
	SharedBudgetID           *string  `json:"sharedBudgetId,omitempty"`
	PerUserLimitMicrodollars *int64   `json:"perUserLimitMicrodollars,omitempty"`
	AllowUnbudgeted          *bool    `json:"allowUnbudgeted,omitempty"`
}
```

In `toBody`, set the new field in both loops (after `AllowedModels`):

```go
		b.RoleMappings = append(b.RoleMappings, roleMappingBody{
			ExternalRole:             rm.ExternalRole.ValueString(),
			GatewayRole:              ptrIf(rm.GatewayRole),
			AllowedModels:            listOrNil(ctx, rm.AllowedModels),
			AllowedProviders:         listOrNil(ctx, rm.AllowedProviders),
			UserBudgetMicrodollars:   int64Ptr(rm.UserBudgetMicrodollars),
			SharedBudgetID:           ptrIf(rm.SharedBudgetID),
			PerUserLimitMicrodollars: int64Ptr(rm.PerUserLimitMicrodollars),
			AllowUnbudgeted:          boolPtr(rm.AllowUnbudgeted),
		})
```

```go
		b.GroupMappings = append(b.GroupMappings, groupMappingBody{
			ExternalGroupID:          gm.ExternalGroupID.ValueString(),
			GatewayRole:              ptrIf(gm.GatewayRole),
			AllowedModels:            listOrNil(ctx, gm.AllowedModels),
			AllowedProviders:         listOrNil(ctx, gm.AllowedProviders),
			TeamID:                   ptrIf(gm.TeamID),
			UserBudgetMicrodollars:   int64Ptr(gm.UserBudgetMicrodollars),
			SharedBudgetID:           ptrIf(gm.SharedBudgetID),
			PerUserLimitMicrodollars: int64Ptr(gm.PerUserLimitMicrodollars),
			AllowUnbudgeted:          boolPtr(gm.AllowUnbudgeted),
		})
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/provider/ -run TestCompanygptIntegration -v`
Expected: PASS (both the new test and the existing `TestCompanygptIntegrationBodyMarshalsCamelCaseAndOmitsUnset` — the latter is unaffected because its mappings leave `AllowedProviders` null → `listOrNil` returns nil → `omitempty` drops it).

- [ ] **Step 7: Commit**

```bash
gofmt -w internal/provider/companygpt_integration_resource.go internal/provider/companygpt_integration_test.go
go test ./internal/provider/...
git add internal/provider/companygpt_integration_resource.go internal/provider/companygpt_integration_test.go
git commit -m "feat(integration): allowed_providers on group + role mappings"
```

---

### Task 2: `aigateway_tenant_settings` — currency, user max, default cost center, last-writer-wins

**Files:**
- Modify: `internal/provider/tenant_settings_resource.go` (model, schema, `tenantPatchBody`, `tenantAPI`, `write`, `Read`)
- Test: create `internal/provider/tenant_settings_resource_test.go`

**Interfaces:**
- Consumes: gateway `PATCH/GET /api/v1/admin/tenant` with new optional fields `currency`, `defaultUserBudgetMicrodollars` (clearable → unlimited when null), `defaultCostCenterId` (clearable), and `managedRevision` (last-writer-wins arbiter). Requires the gateway Phase 1 Task 4 to land first (see cross-plan gaps).
- Produces: `tenantSettingsResourceModel` gains `Currency types.String`, `DefaultUserBudgetMicros types.Int64`, `DefaultUserBudgetUnlimited types.Bool`, `DefaultCostCenterID types.String`, `ManagedRevision types.String`. `write()` emits a fresh RFC3339 `managedRevision` on every apply so the gateway treats Terraform's write as the latest writer only when TF config changed (TF only calls `write` on a real diff; a no-op apply does not call the gateway). `Read()` does NOT refresh the mutable fields (`currency`/user-max/default-cost-center) from the gateway — it leaves the last-applied state value in place so a dashboard edit is not reported as drift and reverted.

- [ ] **Step 1: Write the failing test**

Create `internal/provider/tenant_settings_resource_test.go`:

```go
package provider

import (
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// The PATCH body must carry currency, user-max, default cost center and a
// managed revision. User-max unlimited sends 0 (the gateway clears the cap),
// matching the org-budget-unlimited convention already in this resource.
func TestTenantPatchBodyMarshalsFull(t *testing.T) {
	body := tenantPatchBody{
		DefaultAllowedModels:    []string{"gpt-5.4"},
		OrgBudgetMicros:         0,
		Currency:                "EUR",
		DefaultUserBudgetMicros: 50000000,
		DefaultCostCenterID:     "budget_companygpt",
		ManagedRevision:         "2026-06-21T10:00:00Z",
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	want := `{"defaultAllowedModels":["gpt-5.4"],"orgBudgetLimitMicrodollars":0,"currency":"EUR","defaultUserBudgetMicrodollars":50000000,"defaultCostCenterId":"budget_companygpt","managedRevision":"2026-06-21T10:00:00Z"}`
	if got != want {
		t.Errorf("patch body mismatch\n got: %s\nwant: %s", got, want)
	}
}

// Read must NOT overwrite the configured mutable fields from the gateway
// response (last-writer-wins: a dashboard edit must not be reverted). Only
// the singleton id is reconciled.
func TestTenantSettingsReadDoesNotRevertMutableFields(t *testing.T) {
	state := tenantSettingsResourceModel{
		Currency:                types.StringValue("EUR"),
		DefaultUserBudgetMicros: types.Int64Value(50000000),
		DefaultCostCenterID:     types.StringValue("budget_companygpt"),
	}
	// applyRead simulates a gateway GET that reports DIFFERENT values (a
	// dashboard edit). Read must leave the planned/state values untouched.
	out := tenantAPI{
		Currency:                      "USD",
		DefaultUserBudgetMicrodollars: ptrInt64(999),
		DefaultCostCenterID:           "budget_other",
	}
	applyTenantRead(&state, &out)
	if state.Currency.ValueString() != "EUR" {
		t.Errorf("currency reverted to %q, want EUR", state.Currency.ValueString())
	}
	if state.DefaultUserBudgetMicros.ValueInt64() != 50000000 {
		t.Errorf("user max reverted to %d", state.DefaultUserBudgetMicros.ValueInt64())
	}
	if state.DefaultCostCenterID.ValueString() != "budget_companygpt" {
		t.Errorf("default cost center reverted to %q", state.DefaultCostCenterID.ValueString())
	}
}

func ptrInt64(v int64) *int64 { return &v }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/provider/ -run TestTenant -v`
Expected: FAIL — compile errors: `unknown field Currency in struct literal of type provider.tenantPatchBody`, `undefined: applyTenantRead`, `tenantAPI` has no field `Currency`.

- [ ] **Step 3: Extend the model**

In `tenant_settings_resource.go`, extend the model struct:

```go
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
```

- [ ] **Step 4: Extend the schema**

Add these attributes to the `Attributes` map (after `org_budget_unlimited`):

```go
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
				Optional:    true,
				Computed:    true,
				Description: "Last-writer-wins revision. The provider stamps a fresh value on every apply; the gateway only accepts a write whose revision is newer than the stored one.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
```

Add the imports `planmodifier` and `stringplanmodifier` to the import block:

```go
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
```

- [ ] **Step 5: Extend the wire bodies and write()**

Replace `tenantPatchBody` and `tenantAPI`:

```go
// tenantPatchBody always transmits the managed scalar fields. The gateway
// interprets *BudgetMicrodollars == 0 as "unlimited" (clears the cap); a
// positive value sets the cap. managedRevision is the last-writer-wins arbiter:
// the gateway only applies this write when the revision is >= the stored one.
type tenantPatchBody struct {
	DefaultAllowedModels    []string `json:"defaultAllowedModels"`
	OrgBudgetMicros         int64    `json:"orgBudgetLimitMicrodollars"`
	Currency                string   `json:"currency,omitempty"`
	DefaultUserBudgetMicros int64    `json:"defaultUserBudgetMicrodollars"`
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
```

Add the `time` import to the import block, then update `write()`:

```go
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
		body.DefaultUserBudgetMicros = 0 // 0 → unlimited
	} else {
		body.DefaultUserBudgetMicros = plan.DefaultUserBudgetMicros.ValueInt64()
	}
	// Persist the revision we stamped so it round-trips into state.
	plan.ManagedRevision = types.StringValue(body.ManagedRevision)
	if err := r.client.do(ctx, "PATCH", "/api/v1/admin/tenant", nil, body, nil); err != nil {
		diags.err("Update tenant settings failed", err.Error())
	}
}
```

- [ ] **Step 6: Add applyTenantRead and wire it into Read**

Add this helper (it documents the last-writer-wins contract: mutable fields are NOT refreshed from the gateway):

```go
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
```

Update `Read` to call it (and keep the list conversion where ctx/diags are available):

```go
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
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/provider/ -run TestTenant -v`
Expected: PASS (`TestTenantPatchBodyMarshalsFull`, `TestTenantSettingsReadDoesNotRevertMutableFields`).

- [ ] **Step 8: Commit**

```bash
gofmt -w internal/provider/tenant_settings_resource.go internal/provider/tenant_settings_resource_test.go
go test ./internal/provider/...
git add internal/provider/tenant_settings_resource.go internal/provider/tenant_settings_resource_test.go
git commit -m "feat(tenant-settings): currency, per-user max, default cost center, last-writer-wins via managed_revision"
```

---

### Task 3: `aigateway_cost_center` — mode, caps (daily/weekly/monthly clearable), auto-add, agent, fallback chain

**Files:**
- Modify: `internal/provider/cost_center_resource.go` (model, schema, `costCenterCreateBody`, `costCenterUpdateBody`, `costCenterAPI`, `Create`, `Update`, `apply`)
- Test: `internal/provider/cost_center_resource_test.go`

**Interfaces:**
- Consumes: gateway `POST /api/v1/admin/budgets` (`CreateBudgetRequest`: `monthlyCap`, `dailyCap`, `weeklyCap`, `mode`, `agentId`, `autoAddNewUsers`, `fallbackChain`) and `PATCH /api/v1/admin/budgets/{id}` (`UpdateBudgetRequest`: caps via double-option clear, `fallbackChain`, `autoAddNewUsers`). Requires gateway Phase 1 to add `weeklyCap` to both request structs and `mode`/`agentId` to the update struct (see cross-plan gaps).
- Produces: `costCenterResourceModel` gains `Mode types.String` (tfsdk `mode`), `DailyCap types.String`, `WeeklyCap types.String`, `MonthlyCap` (existing), `AutoAddNewUsers types.Bool`, `AgentID types.String`, `FallbackChain types.List`. The **create body** uses `omitempty` on caps (absent on create = unlimited). The **update body** uses NON-omitempty `*string` caps so a configured-null serialises an explicit JSON `null` (clear). `mode` defaults to `"pool"` when unset.

- [ ] **Step 1: Write the failing tests**

Add to `internal/provider/cost_center_resource_test.go`:

```go
// Create body marshals all the new budget fields in camelCase, omitting unset
// optionals; caps are JSON strings; fallbackChain is an array of budget ids.
func TestCostCenterCreateBodyFullBudget(t *testing.T) {
	monthly := "500.00"
	weekly := "200.00"
	daily := "50.00"
	mode := "per_user"
	agent := "agent_42"
	autoAdd := true
	body := costCenterCreateBody{
		Name:            "customer-a",
		Currency:        "EUR",
		Mode:            &mode,
		MonthlyCap:      &monthly,
		WeeklyCap:       &weekly,
		DailyCap:        &daily,
		AgentID:         &agent,
		AutoAddNewUsers: &autoAdd,
		FallbackChain:   []string{"budget_companygpt"},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	want := `{"name":"customer-a","currency":"EUR","mode":"per_user","monthlyCap":"500.00","weeklyCap":"200.00","dailyCap":"50.00","agentId":"agent_42","autoAddNewUsers":true,"fallbackChain":["budget_companygpt"]}`
	if got != want {
		t.Errorf("create body mismatch\n got: %s\nwant: %s", got, want)
	}
}

// The UPDATE body sends explicit null for a cleared cap (NOT omitempty), so the
// gateway double-option path clears it. A kept cap is a string; an unset cap in
// the model produces null too (clear) — Terraform distinguishes "leave" via not
// diffing, so null here is always "set to this value or clear".
func TestCostCenterUpdateBodyClearsCapWithNull(t *testing.T) {
	monthly := "100.00"
	body := costCenterUpdateBody{
		Name:       strp("customer-a"),
		MonthlyCap: &monthly, // set
		WeeklyCap:  nil,      // cleared → explicit null
		DailyCap:   nil,      // cleared → explicit null
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	want := `{"name":"customer-a","monthlyCap":"100.00","weeklyCap":null,"dailyCap":null}`
	if got != want {
		t.Errorf("update body mismatch\n got: %s\nwant: %s", got, want)
	}
}

func strp(s string) *string { return &s }
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/provider/ -run TestCostCenter -v`
Expected: FAIL — compile errors: `unknown field Mode in struct literal of type provider.costCenterCreateBody`, `unknown field WeeklyCap`, etc.

- [ ] **Step 3: Extend the model**

In `cost_center_resource.go`, replace the model:

```go
type costCenterResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Currency        types.String `tfsdk:"currency"`
	Description     types.String `tfsdk:"description"`
	IsOrg           types.Bool   `tfsdk:"is_org"`
	Mode            types.String `tfsdk:"mode"`
	MonthlyCap      types.String `tfsdk:"monthly_cap"`
	WeeklyCap       types.String `tfsdk:"weekly_cap"`
	DailyCap        types.String `tfsdk:"daily_cap"`
	AutoAddNewUsers types.Bool   `tfsdk:"auto_add_new_users"`
	AgentID         types.String `tfsdk:"agent_id"`
	FallbackChain   types.List   `tfsdk:"fallback_chain"`
}
```

- [ ] **Step 4: Extend the schema**

Add these attributes after `monthly_cap` in the `Attributes` map. NOTE: caps are Optional-only (NO `Computed`, NO `UseStateForUnknown`) so a removed cap plans as null and clears:

```go
			"mode": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Budget mode: pool (one shared counter) | per_user (a counter per member). Defaults to pool.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"weekly_cap": schema.StringAttribute{
				Optional:    true,
				Description: "Weekly cap as a decimal string. \"0\" blocks, a positive number caps, omit/null = unlimited (clearable). Must satisfy daily <= weekly <= monthly when set.",
			},
			"daily_cap": schema.StringAttribute{
				Optional:    true,
				Description: "Daily cap as a decimal string. \"0\" blocks, a positive number caps, omit/null = unlimited (clearable).",
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
```

`mode` is server-defaulted ("pool") so it keeps `UseStateForUnknown` (it is NOT a cap). Caps deliberately have no plan modifier. The `monthly_cap` attribute is already present and stays Optional-only — do not add `UseStateForUnknown` to it.

- [ ] **Step 5: Extend the wire bodies**

```go
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
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Currency        string   `json:"currency"`
	Description     string   `json:"description"`
	Mode            string   `json:"mode"`
	MonthlyCap      *string  `json:"monthlyCap"`
	WeeklyCap       *string  `json:"weeklyCap"`
	DailyCap        *string  `json:"dailyCap"`
	AgentID         *string  `json:"agentId"`
	AutoAddNewUsers bool     `json:"autoAddNewUsers"`
	FallbackChain   []string `json:"fallbackChain"`
	IsOrg           bool     `json:"isOrg"`
}
```

- [ ] **Step 6: Wire Create and Update**

In `Create`, after computing `currency`, build the full body:

```go
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
```

In `Update`, replace the body construction. Caps use `capPtr` (defined below) which returns a pointer-or-nil but the struct fields are non-omitempty so nil → JSON null:

```go
	body := costCenterUpdateBody{
		Name:            ptrIf(plan.Name),
		Description:     ptrIf(plan.Description),
		MonthlyCap:      ptrIf(plan.MonthlyCap),
		WeeklyCap:       ptrIf(plan.WeeklyCap),
		DailyCap:        ptrIf(plan.DailyCap),
		AutoAddNewUsers: boolPtr(plan.AutoAddNewUsers),
		FallbackChain:   listOrNil(ctx, plan.FallbackChain),
	}
```

(`ptrIf` already returns nil for null/unknown/empty strings; combined with the non-omitempty struct tag on the cap fields, that nil marshals to explicit `null` — exactly the clear semantics. `ptrIf` is defined in `provider_resource.go`.)

- [ ] **Step 7: Extend apply()**

Replace `apply` so it reflects the new fields. Caps are Optional-only: only reflect a server value when present, leave the planned/null value otherwise (mirrors the existing `monthly_cap` handling). `mode` is server-defaulted so always reflect it:

```go
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
	if a.Description != "" {
		m.Description = types.StringValue(a.Description)
	}
	if !m.IsOrg.IsNull() && !m.IsOrg.IsUnknown() {
		m.IsOrg = types.BoolValue(a.IsOrg)
	}
	if a.Mode != "" {
		m.Mode = types.StringValue(a.Mode)
	}
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
}
```

Note: `fallback_chain` is Optional-only and NOT reflected here (leave the configured value) — the gateway stores it but reflecting an empty server list onto a null plan would be an inconsistent result, mirroring how the resource treats `description`/`is_org`.

Add the `planmodifier`/`stringplanmodifier` imports (already imported in this file — verify the import block contains both; if missing add them).

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/provider/ -run TestCostCenter -v`
Expected: PASS (the two new tests plus the pre-existing `TestCostCenterCreateBodyMarshalsFull`, `TestCostCenterCreateBodyOmitsOptional`, `TestCostCenterApply*`). NOTE: `TestCostCenterCreateBodyOmitsOptional` still passes — the new create-body fields are all `omitempty` and unset in that test.

- [ ] **Step 9: Commit**

```bash
gofmt -w internal/provider/cost_center_resource.go internal/provider/cost_center_resource_test.go
go test ./internal/provider/...
git add internal/provider/cost_center_resource.go internal/provider/cost_center_resource_test.go
git commit -m "feat(cost-center): mode, daily/weekly/monthly clearable caps, auto_add, agent_id, fallback_chain"
```

---

### Task 4: `aigateway_cost_center` — `sub_limits` reconciled via the sub-limit sub-endpoints

**Files:**
- Modify: `internal/provider/cost_center_resource.go` (model `subLimitModel`, schema nested `sub_limits`, sub-limit wire bodies, `reconcileSubLimits`, call sites in `Create`/`Update`/`Read`)
- Test: `internal/provider/cost_center_resource_test.go`

**Interfaces:**
- Consumes: gateway sub-limit sub-resource endpoints (verified in `ai-gateway/src/routes/admin/budgets/mod.rs`): `POST /api/v1/admin/budgets/{id}/sub-limits` (`CreateSubLimitRequest { scope, capAmount, dailyCap?, weeklyCap?, label? }`), `PATCH /api/v1/admin/budgets/{id}/sub-limits/{subId}` (`UpdateSubLimitRequest`), `DELETE /api/v1/admin/budgets/{id}/sub-limits/{subId}`. The full budget GET returns `subLimits` as an array of `{ id, scope, capAmount, dailyCap?, weeklyCap?, label? }`. There is NO inline `subLimits` field on the budget create/update body — sub-limits MUST be managed as sub-resources. Requires gateway Phase 1 to add `weeklyCap` to the sub-limit model + request bodies (see cross-plan gaps).
- Produces: `subLimitModel { ScopeType, ScopeID, AliasName, CapAmount, DailyCap, WeeklyCap }`; a `reconcileSubLimits(ctx, budgetID, desired []subLimitModel) error` method that GETs current sub-limits, then creates/updates/deletes to match desired (keyed by scope identity). Called after the budget POST/PATCH in Create/Update.

- [ ] **Step 1: Write the failing test**

Add to `internal/provider/cost_center_resource_test.go`:

```go
// A sub-limit create body marshals the scope as an internally-tagged object and
// caps as decimal strings, omitting unset optionals.
func TestSubLimitCreateBodyScopeTagging(t *testing.T) {
	cap := "50.00"
	weekly := "20.00"
	body := subLimitCreateBody{
		Scope:     subLimitScopeBody{Type: "provider", ProviderID: "provider_anthropic"},
		CapAmount: cap,
		WeeklyCap: &weekly,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	want := `{"scope":{"type":"provider","providerId":"provider_anthropic"},"capAmount":"50.00","weeklyCap":"20.00"}`
	if got != want {
		t.Errorf("sub-limit body mismatch\n got: %s\nwant: %s", got, want)
	}
}

// scopeKey produces a stable identity per sub-limit so reconcile can match
// desired against current regardless of server-assigned ids.
func TestSubLimitScopeKey(t *testing.T) {
	cases := []struct {
		m    subLimitModel
		want string
	}{
		{subLimitModel{ScopeType: types.StringValue("provider"), ScopeID: types.StringValue("provider_x")}, "provider:provider_x"},
		{subLimitModel{ScopeType: types.StringValue("model"), ScopeID: types.StringValue("gpt-5.4")}, "model:gpt-5.4"},
		{subLimitModel{ScopeType: types.StringValue("alias"), AliasName: types.StringValue("smart")}, "alias:smart"},
		{subLimitModel{ScopeType: types.StringValue("router"), ScopeID: types.StringValue("router_1")}, "router:router_1"},
	}
	for _, c := range cases {
		if got := c.m.scopeKey(); got != c.want {
			t.Errorf("scopeKey()=%q want %q", got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/provider/ -run TestSubLimit -v`
Expected: FAIL — compile errors: `undefined: subLimitCreateBody`, `subLimitScopeBody`, `subLimitModel`, method `scopeKey`.

- [ ] **Step 3: Add the model + scope key**

In `cost_center_resource.go` add:

```go
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
```

Add `SubLimits []subLimitModel` with tfsdk tag `sub_limits` to `costCenterResourceModel` (after `FallbackChain`):

```go
	SubLimits       []subLimitModel `tfsdk:"sub_limits"`
```

- [ ] **Step 4: Add the schema nested attribute**

Add to the cost center `Attributes` map (after `fallback_chain`):

```go
			"sub_limits": schema.ListNestedAttribute{
				Optional:    true,
				Description: "Fine-grained caps within this budget, scoped to a provider/model/alias/router. Reconciled against the gateway sub-limit sub-resources on every apply.",
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
```

- [ ] **Step 5: Add the sub-limit wire bodies**

```go
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

// Update sends caps NON-omitempty so a nil pointer clears (explicit null),
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
```

- [ ] **Step 6: Implement reconcileSubLimits**

```go
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
```

- [ ] **Step 7: Call reconcileSubLimits from Create and Update**

In `Create`, after `r.apply(&plan, &out, currency)` and before `resp.State.Set`:

```go
	if len(plan.SubLimits) > 0 {
		if err := r.reconcileSubLimits(ctx, out.ID, plan.SubLimits); err != nil {
			resp.Diagnostics.AddError("Reconcile cost center sub-limits failed", err.Error())
			return
		}
	}
```

In `Update`, after `r.apply(&plan, &out, optString(plan.Currency))` and before `resp.State.Set`:

```go
	if err := r.reconcileSubLimits(ctx, state.ID.ValueString(), plan.SubLimits); err != nil {
		resp.Diagnostics.AddError("Reconcile cost center sub-limits failed", err.Error())
		return
	}
```

(Update always reconciles — passing an empty `plan.SubLimits` deletes any that were removed from config.)

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/provider/ -run TestSubLimit -v`
Expected: PASS (`TestSubLimitCreateBodyScopeTagging`, `TestSubLimitScopeKey`).
Then run the full cost-center suite: `go test ./internal/provider/ -run TestCostCenter -v` → still PASS.

- [ ] **Step 9: Commit**

```bash
gofmt -w internal/provider/cost_center_resource.go internal/provider/cost_center_resource_test.go
go test ./internal/provider/...
git add internal/provider/cost_center_resource.go internal/provider/cost_center_resource_test.go
git commit -m "feat(cost-center): sub_limits reconciled via gateway sub-limit sub-resources"
```

---

### Task 5: Docs regen + examples + manual acceptance notes

**Files:**
- Modify: `examples/` (add a cost-center example showing the new fields — follow the existing example layout)
- Create: `docs/superpowers/plans/notes-acceptance.md` (manual TF_ACC checklist; non-shipping doc)
- Regenerate: `docs/` Terraform Registry docs via `tfplugindocs` (the repo already commits generated docs — see commit `d0f1848`)

**Interfaces:**
- Consumes: the schemas produced by Tasks 1–4. No new Go interfaces.
- Produces: registry docs reflecting the new attributes; a runnable example; a manual acceptance checklist (acceptance tests are optional/manual — they need a live gateway with the Phase 1 endpoints).

- [ ] **Step 1: Add a cost-center example**

Inspect the existing examples layout first:

Run: `ls examples && find examples -name '*.tf' | head`

Create `examples/resources/aigateway_cost_center/resource.tf` (match the directory convention you observed; if examples are flat, place it where the others live):

```hcl
resource "aigateway_cost_center" "customer_a" {
  name        = "customer-a"
  currency    = "EUR"
  mode        = "per_user"
  monthly_cap = "500.00"
  weekly_cap  = "200.00"
  daily_cap   = "50.00"

  auto_add_new_users = false
  fallback_chain     = [aigateway_cost_center.companygpt.id]

  sub_limits = [
    {
      scope_type = "provider"
      scope_id   = "provider_anthropic"
      cap_amount = "50.00"
      weekly_cap = "20.00"
    },
    {
      scope_type = "model"
      scope_id   = "gpt-5.4"
      cap_amount = "0" # blocked
    },
  ]
}
```

- [ ] **Step 2: Add the manual acceptance checklist**

Create `docs/superpowers/plans/notes-acceptance.md`:

```markdown
# Cost-Center Phase 1 — Manual Acceptance Checklist (optional, needs live gateway)

Acceptance tests require `TF_ACC=1` and a gateway built from
`ai-gateway` Phase 1 (`weeklyCap`, `cap=0`, `defaultCostCenterId`,
`managedRevision`, sub-limit `weeklyCap`, `mode`/`agentId` on budget update).

Run against a disposable tenant:

1. `terraform apply` a cost center with monthly/weekly/daily caps + two
   sub-limits → confirm `GET /budgets/{id}?include=...` shows all caps and the
   sub-limits with correct scopes.
2. Remove `weekly_cap` from config, re-apply → confirm the gateway cleared it
   (budget detail `weeklyCap` is null), monthly/daily untouched.
3. Set a sub-limit `cap_amount = "0"`, apply, call a matching model → expect a
   budget-exceeded rejection (cap=0 = blocked).
4. `tenant_settings`: set `currency`/user-max/default cost center, apply; then
   edit currency in the dashboard; re-run `terraform plan` → expect NO diff
   (last-writer-wins, Read does not revert).
5. `group_mappings` with `allowed_providers = ["provider_openai"]` → a user in
   that group calling an Anthropic model is rejected.
```

- [ ] **Step 3: Regenerate registry docs**

Run: `go generate ./...` (if the repo wires `tfplugindocs` via a `//go:generate` directive) OR `tfplugindocs generate` if installed.
Expected: `docs/resources/aigateway_cost_center.md`, `docs/resources/aigateway_tenant_settings.md`, `docs/resources/aigateway_companygpt_integration.md` updated with the new attributes.
If `tfplugindocs` is not installed, skip generation and note it in the commit body — the schemas carry the descriptions, docs can be regenerated at release time.

- [ ] **Step 4: Build + vet + test**

Run: `go build ./... && go vet ./... && go test ./internal/provider/...`
Expected: build succeeds, vet clean, all unit tests pass.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/provider/
git add examples docs
git commit -m "docs(cost-center): examples + registry docs + manual acceptance checklist for phase 1"
```

---

## Release (after all tasks merge)

- [ ] Open a PR `feat: cost-center phase 1 (cost-center budget model, tenant settings currency/user-max/default-cc, integration allowed_providers)`; squash-merge to `main`.
- [ ] **Rollout ordering (standing rule):** the gateway Phase 1 must be merged, tagged, and its images published+verified in GAR FIRST. Then tag this provider; only after the provider release artifacts are published+verified does companyGPT pin both versions. Never pin in companyGPT before the registry artifacts exist (avoids rollout-timeout / null-identity corruption).
- [ ] Tag the provider minor: `git tag v0.7.0 && git push origin v0.7.0`. GoReleaser stamps `main.version=0.7.0` via ldflags and publishes signed archives + `SHA256SUMS` to the GitHub release the Terraform Registry consumes. There is no in-repo version constant to bump.
- [ ] Verify `v0.7.0` appears on the Terraform Registry (or the private registry the tenants use) before any companyGPT `required_providers` pins `= 0.7.0`.

## Cross-plan gaps (flag before integration testing)

The provider unit tests in this plan stand alone, but live integration needs the
gateway Phase 1 plan to deliver these — which its current task list does NOT
fully cover. Raise these against `ai-gateway/docs/superpowers/plans/2026-06-21-gateway-cost-center-phase1.md`:

1. **`weeklyCap` is not added to the budget request bodies.** Gateway Task 1 adds
   `weekly_cap` to the `Budget`/`SubLimit` *models* + validation, but
   `CreateBudgetRequest`/`UpdateBudgetRequest` (in `routes/admin/budgets/mod.rs`)
   have no `weekly_cap` field, and the create handler builds `Budget { ... }`
   without it. The provider Task 3/4 sends `weeklyCap`; the gateway will silently
   ignore it until the request structs + create/update handlers map it.
2. **`mode` and `agentId` are not in `UpdateBudgetRequest`.** They exist on
   `CreateBudgetRequest` only. The provider's `costCenterUpdateBody` omits both
   (matching today's gateway), so editing `mode`/`agent_id` in Terraform after
   create is a no-op server-side (mode has its own `/budgets/{id}/mode` route).
   Either accept create-only semantics (document it) or have the gateway accept
   them on PATCH. The plan currently treats `mode` as `UseStateForUnknown` and
   does not send it on update — consistent with create-only.
3. **Sub-limit `weeklyCap` request mapping.** Gateway Task 1 adds `weekly_cap` to
   the `SubLimit` model, but `CreateSubLimitRequest`/`UpdateSubLimitRequest` do
   not carry it yet. The provider Task 4 sends `weeklyCap` on the sub-limit
   create/update bodies; the gateway must add it to those request structs +
   handlers.
4. **No inline `subLimits` on the budget body.** The design (companyGPT spec
   §4.2) implies sub-limits travel with the cost center; the gateway exposes them
   only as sub-resources (`/budgets/{id}/sub-limits`). This plan reconciles them
   via those endpoints (Task 4) rather than waiting for an inline field — no
   gateway change required, but the design's wording is looser than the API.
5. **`auto_add_new_users` clears via PATCH but `seed_all_existing_users`
   (one-shot bulk-add) is gateway-only.** The design §4.1 mentions an optional
   one-shot TF flag that triggers `POST /budgets/{id}/members:add-all`. This plan
   does NOT add that flag (it is not in the SCOPE list and the bulk-add endpoint
   is the gateway's Task 5). If desired later, it is a small follow-up attribute
   on `aigateway_cost_center`.

These are integration gaps, not blockers for landing the provider changes +
unit tests; they must be closed gateway-side before the live acceptance
checklist (Task 5) passes.
```
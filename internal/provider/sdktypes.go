package provider

import aigateway "github.com/innFactory/aigateway-go"

// This file is the single place the provider's wire-type NAMES are bound to the
// shared SDK's structs. The provider keeps all its Terraform coupling (the
// *ResourceModel structs with tfsdk tags, schema definitions, plan modifiers and
// the types.*↔native converters); only the JSON request/response types and the
// transport move to the SDK. Each alias below points at the exact SDK struct the
// provider used to define inline, so resource code and tests read unchanged while
// the field definitions live in exactly one repo.

// ── providers ────────────────────────────────────────────────────────────────
type (
	providerCreateBody = aigateway.ProviderCreateBody
	providerUpdateBody = aigateway.ProviderUpdateBody
	providerAPI        = aigateway.Provider
)

// ── models ───────────────────────────────────────────────────────────────────
type (
	modelCreateBody = aigateway.ModelCreateBody
	modelUpdateBody = aigateway.ModelUpdateBody
	modelAPI        = aigateway.Model
)

// ── api keys ──────────────────────────────────────────────────────────────────
type (
	apiKeyCreateBody     = aigateway.KeyCreateBody
	apiKeyUpdateBody     = aigateway.KeyUpdateBody
	apiKeyCreateResponse = aigateway.CreateKeyResponse
	apiKeyListEntry      = aigateway.APIKey
)

// ── cost centers (budgets) + sub-limits ──────────────────────────────────────
type (
	costCenterCreateBody = aigateway.BudgetCreateBody
	costCenterUpdateBody = aigateway.BudgetUpdateBody
	costCenterAPI        = aigateway.Budget

	subLimitScopeBody  = aigateway.SubLimitScope
	subLimitScopeAPI   = aigateway.SubLimitScope
	subLimitCreateBody = aigateway.SubLimitCreateBody
	subLimitUpdateBody = aigateway.SubLimitUpdateBody
	subLimitAPI        = aigateway.SubLimit
)

// ── tenant settings ──────────────────────────────────────────────────────────
type (
	tenantPatchBody = aigateway.TenantPatchBody
	tenantAPI       = aigateway.TenantSettings
)

// ── deployment groups ────────────────────────────────────────────────────────
type (
	deploymentBody      = aigateway.DeploymentBody
	retryBody           = aigateway.RetryPolicyBody
	cooldownBody        = aigateway.CooldownConfigBody
	deploymentGroupBody = aigateway.DeploymentGroupBody
	deploymentGroupAPI  = aigateway.DeploymentGroup
)

// ── fallback chains ──────────────────────────────────────────────────────────
type fallbackChainBody = aigateway.FallbackChainBody

// ── companyGPT integration ───────────────────────────────────────────────────
type (
	roleMappingBody             = aigateway.IntegrationRoleMapping
	groupMappingBody            = aigateway.IntegrationGroupMapping
	integrationMetadataBody     = aigateway.IntegrationMetadata
	upsertIntegrationPolicyBody = aigateway.CompanygptIntegrationBody
)

// ── access groups (teams) ────────────────────────────────────────────────────
type (
	accessGroupCreateBody = aigateway.TeamCreateBody
	accessGroupUpdateBody = aigateway.TeamUpdateBody
	accessGroupAPI        = aigateway.Team
)

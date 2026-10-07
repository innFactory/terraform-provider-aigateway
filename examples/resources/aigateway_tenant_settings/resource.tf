resource "aigateway_tenant_settings" "this" {
  org_budget_unlimited   = true
  default_allowed_models = ["gpt-5.4-mini", "gpt-5.4", "gemini-3.5-flash", "claude-haiku-4-5"]

  # Optional: access group applied to callers in no other group — scopes
  # /v1/models for trusted-header (LibreChat) users.
  default_access_group_id = aigateway_access_group.librechat_default.id

  # Optional: zero the reseller cost-margins so customer_cost == provider_cost
  # (e.g. internal tenants). Omit both to keep the gateway defaults (20% / 25000).
  azure_commission_percent                   = 0
  external_margin_per_1m_tokens_microdollars = 0

  # Optional, gateway >= 1.1.4: end-user OIDC tokens may not call /v1 and /mcp
  # directly (LibreChat trusted-header traffic and API keys are unaffected).
  oidc_proxy_direct_bearer = "deny"
}

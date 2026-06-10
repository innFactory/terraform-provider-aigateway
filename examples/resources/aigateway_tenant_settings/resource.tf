resource "aigateway_tenant_settings" "this" {
  org_budget_unlimited   = true
  default_allowed_models = ["gpt-5.4-mini", "gpt-5.4", "gemini-3.5-flash", "claude-haiku-4-5"]
}

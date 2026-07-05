# A default access group for trusted-header (LibreChat) users: scopes what
# /v1/models returns for callers in no other group. Wire it up via
# aigateway_tenant_settings.default_access_group_id.
resource "aigateway_access_group" "librechat_default" {
  name           = "librechat-default"
  description    = "Default model bundle for CompanyGPT / LibreChat users"
  icon           = "💬"
  allowed_models = ["gpt-5.4-mini", "gpt-5.4", "claude-haiku-4-5"]
}

# A restricted group mapped to an Entra group: members are auto-assigned on
# login, capped at 60 rpm and 50 USD/month, and may only use the OpenAI wire
# format.
resource "aigateway_access_group" "contractors" {
  name                      = "contractors"
  description               = "External contractors"
  entra_group_id            = "00000000-0000-0000-0000-000000000001"
  budget_limit_microdollars = 50000000 # 50 USD
  rate_limit_rpm            = 60
  allowed_models            = ["gpt-5.4-mini"]
  supported_formats         = ["openai"]
}

# A disabled (parked) group.
resource "aigateway_access_group" "experiments" {
  name    = "experiments"
  enabled = false
}

resource "aigateway_tenant_settings" "this" {
  org_budget_unlimited    = true
  default_access_group_id = aigateway_access_group.librechat_default.id
}

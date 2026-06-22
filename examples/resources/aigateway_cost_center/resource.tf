resource "aigateway_cost_center" "companygpt" {
  name     = "companygpt"
  currency = "EUR"
  # no monthly_cap → attribution-only (unlimited)
}

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

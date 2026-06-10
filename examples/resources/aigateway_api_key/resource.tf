resource "aigateway_api_key" "librechat" {
  name = "librechat"
  # no budget_microdollars → unlimited
}

output "librechat_gateway_key" {
  value     = aigateway_api_key.librechat.key
  sensitive = true
}

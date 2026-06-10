resource "aigateway_model" "gpt_5_4_mini" {
  model_id          = "gpt-5.4-mini"
  display_name      = "GPT-5.4 mini"
  provider_id       = aigateway_provider.azure.id
  provider_model_id = "gpt-5.4-mini"
  deployment_name   = "gpt-5.4-mini"
  capability        = "chat"
}

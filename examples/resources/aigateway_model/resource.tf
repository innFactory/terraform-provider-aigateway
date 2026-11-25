resource "aigateway_model" "gpt_5_4_mini" {
  model_id          = "gpt-5.4-mini"
  display_name      = "GPT-5.4 mini"
  provider_id       = aigateway_provider.azure.id
  provider_model_id = "gpt-5.4-mini"
  deployment_name   = "gpt-5.4-mini"
  capability        = "chat"
}

# Seed the token windows. Omit either attribute to keep whatever the gateway
# already holds (e.g. a value set in the dashboard); gateway >= 1.1.0.
resource "aigateway_model" "gpt_5_4_mini_windows" {
  model_id          = "gpt-5.4-mini-windows"
  display_name      = "GPT-5.4 mini (windows)"
  provider_id       = aigateway_provider.azure.id
  provider_model_id = "gpt-5.4-mini"
  deployment_name   = "gpt-5.4-mini"
  capability        = "chat"

  context_window    = 400000
  max_output_tokens = 128000

  # Gateway >= 1.17.2: price of ONE provider-run web search, in microdollars.
  search_per_query_microdollars = 14000
}

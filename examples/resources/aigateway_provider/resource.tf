resource "aigateway_provider" "azure" {
  type        = "azure_openai"
  name        = "Azure OpenAI 🇪🇺"
  endpoint    = "https://my-aoai.openai.azure.com"
  auth_type   = "apiKey"
  credential  = var.azure_openai_api_key
  api_version = "2024-10-21"
}

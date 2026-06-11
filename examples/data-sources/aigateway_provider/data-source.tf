data "aigateway_provider" "azure" {
  name = "Azure OpenAI 🇪🇺"
}

# Reference the looked-up provider id elsewhere:
# provider_id = data.aigateway_provider.azure.id

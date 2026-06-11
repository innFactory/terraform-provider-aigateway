# Load-balance gpt-5.4 across two Azure regions, weighted, with a retry policy.
resource "aigateway_deployment_group" "gpt_5_4" {
  model_id = aigateway_model.gpt_5_4.model_id
  strategy = "weighted_random"

  deployments = [
    {
      provider_id       = aigateway_provider.azure_swedencentral.id
      provider_model_id = "gpt-5.4"
      deployment_name   = "gpt-5.4"
      weight            = 70
    },
    {
      provider_id       = aigateway_provider.azure_westeurope.id
      provider_model_id = "gpt-5.4"
      deployment_name   = "gpt-5.4"
      weight            = 30
      priority          = 1
    },
  ]

  retry_policy = {
    max_retries           = 3
    total_timeout_seconds = 120
  }
}

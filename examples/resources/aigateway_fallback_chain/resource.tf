# If gpt-5.4's own deployments are exhausted, fall back to these in order.
resource "aigateway_fallback_chain" "gpt_5_4" {
  model_id        = aigateway_model.gpt_5_4.model_id
  fallback_models = ["gpt-5.4-mini", "gpt-4.1"]
}

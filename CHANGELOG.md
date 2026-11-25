# Changelog

## v0.12.0 (unreleased)

- cost_center: `model_access` (`allowed_models`, `allowed_providers`; gateway >= 1.17.2), the per-cost-center model allowlist. Omitted means Terraform does not manage it: nothing is sent and nothing is read back. An allowlist that lists nothing fails `terraform plan`. Removing the block does not clear the list on the gateway; clear it in the dashboard.
- model: `search_per_query_microdollars`, the model's own price per provider-run web search, per one search. The admin API has long had the field; gateway 1.17.2 starts using it (with list-price fallbacks) to bill provider-run web search and grounding. Omitted means not managed.

## v0.11.0

- model: `context_window` and `max_output_tokens` (gateway >= 1.1.0); omitted keeps the stored value. An attribute unset in the configuration is not sent on create, update or adopt, and never sent as null, so a window set in the dashboard is not cleared. Removing the attribute keeps the current value; clear it in the dashboard. A gateway older than 1.1.0 is reported with a clear error instead of an inconsistent-result error.

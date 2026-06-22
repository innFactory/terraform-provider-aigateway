# Cost-Center Phase 1 — Manual Acceptance Checklist (optional, needs live gateway)

Acceptance tests require `TF_ACC=1` and a gateway built from
`ai-gateway` Phase 1 (`weeklyCap`, `cap=0`, `defaultCostCenterId`,
`managedRevision`, sub-limit `weeklyCap`, `mode`/`agentId` on budget update).

Run against a disposable tenant:

1. `terraform apply` a cost center with monthly/weekly/daily caps + two
   sub-limits → confirm `GET /budgets/{id}?include=...` shows all caps and the
   sub-limits with correct scopes.
2. Remove `weekly_cap` from config, re-apply → confirm the gateway cleared it
   (budget detail `weeklyCap` is null), monthly/daily untouched.
3. Set a sub-limit `cap_amount = "0"`, apply, call a matching model → expect a
   budget-exceeded rejection (cap=0 = blocked).
4. `tenant_settings`: set `currency`/user-max/default cost center, apply; then
   edit currency in the dashboard; re-run `terraform plan` → expect NO diff
   (last-writer-wins, Read does not revert).
5. `group_mappings` with `allowed_providers = ["provider_openai"]` → a user in
   that group calling an Anthropic model is rejected.

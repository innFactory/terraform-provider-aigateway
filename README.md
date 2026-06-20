# terraform-provider-aigateway

A Terraform provider for the **innFactory AI Gateway**. Configure an online
gateway declaratively: upstream providers, models, API keys, the org budget,
and the tenant default-allowed-models — all over the gateway admin API using a
single full-admin API key.

## Why

companyGPT (and other deployments) already deploy and configure AI models in
Terraform. This provider lets the same Terraform run **pre-configure the
gateway to match**: the models you deploy become gateway models, an API key is
minted for LibreChat, and the org budget is set unlimited — without any manual
dashboard clicks.

## Usage

```hcl
terraform {
  required_providers {
    aigateway = {
      source  = "innFactory/aigateway"
      version = "~> 0.1"
    }
  }
}

provider "aigateway" {
  endpoint = "https://gateway.example.com"
  # admin_api_key via env AIGATEWAY_ADMIN_API_KEY (matches GATEWAY_ADMIN_API_KEY on the gateway)
}

resource "aigateway_provider" "azure" {
  type        = "azure_openai"
  name        = "Azure OpenAI"
  endpoint    = "https://my-aoai.openai.azure.com"
  credential  = var.azure_openai_api_key
  api_version = "2024-10-21"
}

resource "aigateway_model" "gpt_5_4_mini" {
  model_id          = "gpt-5.4-mini"
  display_name      = "GPT-5.4 mini"
  provider_id       = aigateway_provider.azure.id
  provider_model_id = "gpt-5.4-mini"
  deployment_name   = "gpt-5.4-mini"
}

resource "aigateway_tenant_settings" "this" {
  org_budget_unlimited   = true
  default_allowed_models = [aigateway_model.gpt_5_4_mini.model_id]
}

resource "aigateway_api_key" "librechat" {
  name = "librechat"
}

output "gateway_key" {
  value     = aigateway_api_key.librechat.key
  sensitive = true
}
```

## Authentication

The provider authenticates with the gateway's **full-admin API key**
(`GATEWAY_ADMIN_API_KEY` on the gateway). A request bearing this key is granted
an Owner context on `/api/v1/admin/*`. Set it via `AIGATEWAY_ADMIN_API_KEY`.

The gateway must be **online and reachable** during `terraform apply` (the
provider drives the live API). For an in-cluster/HTTP endpoint set
`AIGATEWAY_INSECURE_ENDPOINT=1`.

## Resources

| Resource | Manages | Key |
|---|---|---|
| `aigateway_provider` | upstream provider (azure_openai, anthropic, gemini, …) | server `id` |
| `aigateway_model` | a model bound to a provider | caller `model_id` |
| `aigateway_api_key` | a gateway API key (plaintext returned once) | server `id` |
| `aigateway_tenant_settings` | default allowed models + org budget (unlimited) | singleton |
| `aigateway_deployment_group` | load-balance one model across many provider deployments (multi-region/provider) with strategy + retry + cooldown | per `model_id` |
| `aigateway_fallback_chain` | ordered fallback models tried after a model's deployments are exhausted | per `model_id` |
| `aigateway_companygpt_integration` | the companyGPT integration policy: enables the trusted-header + direct-OIDC integration and maps Entra groups → gateway role / model allowlist | per `tenant_id` |

### `aigateway_companygpt_integration`

Manages the per-tenant **companyGPT integration policy** that turns on the
companyGPT/LibreChat integration and wires **Entra-group RBAC**. It writes to a
single **PUT-only** admin endpoint
(`PUT /api/v1/admin/tenant/{id}/companygpt-integration`, OwnerOnly), so the
resource is **write-mostly**: Create/Update both PUT the full policy and
`Delete` **deactivates** the integration (`enabled = false`) rather than
removing tenant data.

```hcl
resource "aigateway_companygpt_integration" "this" {
  tenant_id              = "default"
  enabled                = true
  allow_unbudgeted_users = true   # auto-provision all authenticated users
  # external_tenant_ids  = [...]  # omit to accept any external tenant

  # Coarse role-string → gateway role (capped to member/guest).
  role_mappings = [
    { role = "admin", gateway_role = "member" },
  ]

  # Entra group → gateway role + optional model allowlist. Group object ids MAY
  # grant admin/owner (pinned to explicit ids). Empty allowed_models = all.
  group_mappings = [
    { external_group_id = var.admin_group_id, gateway_role = "owner" },
    { external_group_id = var.user_group_id,  gateway_role = "member" },
  ]

  managed_by = "companygpt-terraform"
}
```

Key attributes:

| Attribute | Description |
|---|---|
| `tenant_id` | tenant the policy applies to (e.g. `default`) |
| `enabled` | turns the integration on; `Delete` sets this `false` |
| `allow_unbudgeted_users` | auto-provision authenticated users without a budget |
| `external_tenant_ids` | external (LibreChat) tenant ids to accept; omit for any |
| `role_mappings` | coarse role-string → gateway role (capped to member/guest) |
| `group_mappings` | Entra group → `gateway_role` (+ optional `allowed_models`); group ids may grant admin/owner |
| `managed_by` | free-form owner tag (e.g. `companygpt-terraform`) |

## Data sources

| Data source | Looks up |
|---|---|
| `aigateway_provider` | an existing provider by `id` or `name` |
| `aigateway_model` | an existing model by `model_id` |

## Development

```bash
go build ./...
go vet ./...
go test ./...
```

Release is GoReleaser-driven on a `v*` tag (`.github/workflows/release.yml`),
publishing signed binaries for the Terraform Registry.

Licensed MPL-2.0.

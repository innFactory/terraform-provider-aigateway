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
  endpoint = "https://innfactory26.aigateway.agentic-web.eu"
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

## Development

```bash
go build ./...
go vet ./...
go test ./...
```

Release is GoReleaser-driven on a `v*` tag (`.github/workflows/release.yml`),
publishing signed binaries for the Terraform Registry.

Licensed MPL-2.0.

terraform {
  required_providers {
    aigateway = {
      source  = "innFactory/aigateway"
      version = "~> 0.1"
    }
  }
}

provider "aigateway" {
  # endpoint   = "https://innfactory26.aigateway.agentic-web.eu"   # or env AIGATEWAY_ENDPOINT
  # admin_api_key supplied via env AIGATEWAY_ADMIN_API_KEY (recommended)
}

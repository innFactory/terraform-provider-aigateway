terraform {
  required_providers {
    aigateway = {
      source  = "innFactory/aigateway"
      version = "~> 0.1"
    }
  }
}

provider "aigateway" {
  # endpoint   = "https://gateway.example.com"   # or env AIGATEWAY_ENDPOINT
  # admin_api_key supplied via env AIGATEWAY_ADMIN_API_KEY (recommended)
}

provider "regapi" {
  # Defaults to https://api.regapi.dev.
  # endpoint = "https://api.staging.regapi.dev"

  # Or set the REGAPI_API_KEY environment variable.
  api_key = var.regapi_api_key
}

variable "regapi_api_key" {
  type      = string
  sensitive = true
}

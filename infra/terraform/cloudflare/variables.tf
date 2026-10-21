variable "api_token" {
  type        = string
  default     = "placeholder"
  description = "Unused while enable_cloudflare_dns is false. Keep the placeholder. Do not put a live Cloudflare token here."
}

variable "enable_cloudflare_dns" {
  type        = bool
  default     = false
  description = "Create the Cloudflare CNAME. Default false: DNS stays at GoDaddy and no Cloudflare API call is required. Not a Load Balancing toggle. Load Balancing resources are not in this root."
}

variable "zone_id" {
  type        = string
  default     = "00000000000000000000000000000000"
  description = "Cloudflare zone id. Read only if enable_cloudflare_dns is true."
}

variable "hostname" {
  type        = string
  default     = "hybridcloud"
  description = "Relative record name inside a Cloudflare zone. Unused while enable_cloudflare_dns is false. The live name is the GoDaddy subdomain hybridcloud.vellamsetti.com."
}

variable "facade_hostname" {
  type        = string
  default     = "facade.example.com"
  description = "Public hostname of the facade. GoDaddy CNAME hybridcloud.vellamsetti.com points here. This root does not create that host."
}

variable "api_token" {
  type    = string
  default = "placeholder"
}

variable "zone_id" {
  type    = string
  default = "00000000000000000000000000000000"
}

variable "hostname" {
  type    = string
  default = "app"
}

variable "facade_target" {
  type        = string
  default     = "facade.example.com"
  description = "Stable facade hostname. Health-checked DNS failover is a separate decision."
}

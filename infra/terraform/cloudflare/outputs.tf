output "facade_hostname" {
  description = "CNAME target for the GoDaddy record hybridcloud.vellamsetti.com. DNS is not managed by this root."
  value       = var.facade_hostname
}

output "public_name" {
  description = "Subdomain only. The apex vellamsetti.com stays on the existing GoDaddy records."
  value       = "hybridcloud.vellamsetti.com"
}

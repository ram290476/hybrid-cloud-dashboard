# Public DNS is a GoDaddy CNAME for the subdomain hybridcloud.vellamsetti.com.
# The target is output facade_hostname. Nameservers stay at GoDaddy.
# This root does not create Cloudflare Load Balancing pools, monitors, or
# round-robin records. The record below is a future option only: it stays
# uncreated while enable_cloudflare_dns is false (the default). Turning it on
# would require the zone to be hosted at Cloudflare, which this decision rejects.
# Cross-backend failover is the facade routing policy, not DNS.
# TLS for hybridcloud.vellamsetti.com terminates on the facade. This root does
# not request or store a certificate.

resource "cloudflare_record" "app" {
  count = var.enable_cloudflare_dns ? 1 : 0

  zone_id = var.zone_id
  name    = var.hostname
  value   = var.facade_hostname
  type    = "CNAME"
  proxied = false
  ttl     = 120
}

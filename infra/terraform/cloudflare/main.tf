resource "cloudflare_record" "app" {
  zone_id = var.zone_id
  name    = var.hostname
  value   = var.facade_target
  type    = "CNAME"
  proxied = false
  ttl     = 120
}

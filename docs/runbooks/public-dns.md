# Public DNS

DNS for the dashboard is a single CNAME at GoDaddy. Cloudflare is not the DNS host, and Cloudflare Load Balancing is not in use.

## Name

| Side | Value |
| --- | --- |
| Name clients type | `hybridcloud.vellamsetti.com` |
| Record | CNAME, host `hybridcloud` only |
| Target | The facade's public hostname |

The apex `vellamsetti.com` is not part of this record. Do not add an A, AAAA, or CNAME on the apex for the dashboard. Do not add round-robin records for the Go, Python, or .NET backends.

Nameservers for `vellamsetti.com` stay at GoDaddy. Do not move them to Cloudflare.

## CNAME target

The target is the facade's public hostname, not a backend URL. Terraform exposes it and does not create the GoDaddy record:

```bash
terraform -chdir=infra/terraform/cloudflare output facade_hostname
```

The default value is the placeholder `facade.example.com`. Set `facade_hostname` to the real facade host before creating the GoDaddy record. `enable_cloudflare_dns` stays **false**. Validate only. Do not apply.

In GoDaddy DNS for `vellamsetti.com`:

1. Add a CNAME.
2. Host: `hybridcloud`.
3. Points to: the `facade_hostname` output.
4. Leave the record DNS-only. Do not proxy it through Cloudflare.

## TLS

Clients use `https://hybridcloud.vellamsetti.com`. TLS terminates at the facade. The facade presents its own certificate for that name. Cloudflare does not terminate TLS for this host, because the zone is not on Cloudflare nameservers and the CNAME is not proxied.

This repo does not request or store that certificate.

## Failover

DNS has one target and does not health-check the three backends. When a backend is slow, failing, or over its budget, the facade shifts weight across `gcp-go`, `aws-python`, and `azure-dotnet`. That policy is `facade/policy/routing-policy.yaml`. A color flip inside one cloud (Cloud Run revision, Lambda alias, or App Service app) stays inside that environment and does not change the GoDaddy record.

## Future option

Cloudflare Load Balancing, health-checked DNS failover, and a Cloudflare-hosted CNAME are not configured. `infra/terraform/cloudflare` can create a grey-cloud CNAME only when `enable_cloudflare_dns` is set to true. That flag defaults to false. Turning it on would mean hosting the zone at Cloudflare, which this decision does not do. The root declares no load balancer, pool, or monitor.

# Investor-dashboard sync log

Shared market UI is copied from `ram290476/investor-dashboard` `main`. The next sync starts after the latest entry.

## 2026-10-29 — `f9805e4`

Reviewed `9721ca33048f5fae9710aa2d8814fea4b3ca153d..f9805e45c75ad7b09f438ff77cc38e7fbeb12e67` (one commit). The previous sync stopped at `9721ca3` (#71).

**Upstream:** `f9805e45c75ad7b09f438ff77cc38e7fbeb12e67` — "Remove legacy invdash-config-recorder role and default VPC/SG Terraform (#327) (#328)".

**Files:** `infra/terraform/modules/audit_logging/main.tf`, `infra/terraform/modules/security_services/main.tf`, `infra/terraform/tests/plan_time_count.tftest.hcl`, `infra/terraform/tests/security_hub_fixes.tftest.hcl`.

**Ported:** nothing.

**Skipped:** the commit deletes investor-dashboard's unused `${name}-config-recorder` IAM role (and its `AWS_ConfigRole` attachment and delivery policy) and forgets an adopted default security group after that account's default VPC was deleted. This repo has no counterpart. `infra/terraform/aws` declares the Python Lambda Function URL, the S3 serving bucket, the DynamoDB prefs table, a Cognito user pool, and the GitHub OIDC role `hcd-github-actions`. There is no Config recorder, no `aws_vpc` / `aws_default_security_group`, and no audit-logging or security-services module. The GCP, Azure, Cloudflare, and lab roots do not declare those resources either.

**Hybrid behavior kept:** `frontend/hybrid.html`, local `config.json` dev auth (`authMode: dev`, `DEV_INJECT_TOKEN`), the facade, and `hcd-sync` are unchanged.

Issue #72.

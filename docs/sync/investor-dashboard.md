# Investor-dashboard sync log

Shared market UI is copied from `ram290476/investor-dashboard` `main`. The next sync starts after the latest entry.

## 2026-11-14 — `28afeec`

Reviewed `f9805e45c75ad7b09f438ff77cc38e7fbeb12e67..28afeecd94e2a789010930da5b1d84a185d2f447` (three commits). The previous sync stopped at `f9805e4` (#73).

**Upstream:** `28afeecd94e2a789010930da5b1d84a185d2f447`.

**Ported:** `6e20ad7` adds a Research rendering test for two November pattern cards (EVgo gross profit inflection to growth, Alphabet operating income inflection to decline plus sustained deceleration). The cards stay pending review, the quarterly detail stays closed, and a `hide_when_empty` card with no reported value is left off the page. The test lives in `frontend/deep-dive.test.js`. The renderer already draws headline cards from a `deep_dive` payload, so no shared module changed.

**Skipped:**

- `6e20ad7` also edits investor-dashboard `services/data-jobs`: `company_ir/catalog/EVGO.json`, `company_ir/catalog/GOOG.json`, and `tests/test_deep_dive.py`. This repo has no company IR catalog and no deep-dive assembler. The three backends serve dashboard, status, chart, and prefs fixtures. They do not emit a gross-profit or operating-income section.
- `635594c` debounce in `dashboard_build.py` (and `test_dashboard_build.py`) coalesces EventBridge "Job Finished" events into a pending flag and lets a weekday schedule publish the lake snapshot. `news_sentiment.py` (and `test_news_sentiment.py`) keeps one curated article object per day and skips unchanged `news_daily` partitions. This repo has no `services/data-jobs` package, no lake reader, and no news curator. `backends/python-aws` is the dashboard API.
- `635594c` and `28afeec` edit investor-dashboard AWS Terraform and the docs that describe that account: CloudTrail Insights off, write-only lake data events, one `resources.ARN` field selector shared by `starts_with` and `not_starts_with` (deploy `43372765461` rejected a repeated field), Glacier after 90 days, GuardDuty S3 data events left `DISABLED`, malware scanning limited to `raw/company_ir/` and `raw/fund_filings/`. `test_security_cost_tf.py` asserts that HCL. This repo's AWS root (`infra/terraform/aws`) is the Python Lambda Function URL, an S3 serving bucket, a DynamoDB prefs table, a Cognito user pool, and the GitHub OIDC role `hcd-github-actions`. It has no CloudTrail trail, no GuardDuty detector, no data-lake bucket, and no audit-logging or security-services module. The GCP, Azure, Cloudflare, and lab roots do not declare those resources either. `docs/architecture-infrastructure.md`, `docs/operations.md`, and `infra/terraform/README.md` do not exist here; the prose only documents the skipped account.

**Hybrid behavior kept:** `frontend/hybrid.html`, local `config.json` dev auth (`authMode: dev`, `DEV_INJECT_TOKEN`), the facade, and `hcd-sync` are unchanged.

Issue #74.

## 2026-10-29 — `f9805e4`

Reviewed `9721ca33048f5fae9710aa2d8814fea4b3ca153d..f9805e45c75ad7b09f438ff77cc38e7fbeb12e67` (one commit). The previous sync stopped at `9721ca3` (#71).

**Upstream:** `f9805e45c75ad7b09f438ff77cc38e7fbeb12e67` — "Remove legacy invdash-config-recorder role and default VPC/SG Terraform (#327) (#328)".

**Files:** `infra/terraform/modules/audit_logging/main.tf`, `infra/terraform/modules/security_services/main.tf`, `infra/terraform/tests/plan_time_count.tftest.hcl`, `infra/terraform/tests/security_hub_fixes.tftest.hcl`.

**Ported:** nothing.

**Skipped:** the commit deletes investor-dashboard's unused `${name}-config-recorder` IAM role (and its `AWS_ConfigRole` attachment and delivery policy) and forgets an adopted default security group after that account's default VPC was deleted. This repo has no counterpart. `infra/terraform/aws` declares the Python Lambda Function URL, the S3 serving bucket, the DynamoDB prefs table, a Cognito user pool, and the GitHub OIDC role `hcd-github-actions`. There is no Config recorder, no `aws_vpc` / `aws_default_security_group`, and no audit-logging or security-services module. The GCP, Azure, Cloudflare, and lab roots do not declare those resources either.

**Hybrid behavior kept:** `frontend/hybrid.html`, local `config.json` dev auth (`authMode: dev`, `DEV_INJECT_TOKEN`), the facade, and `hcd-sync` are unchanged.

Issue #72.

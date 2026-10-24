# AGENTS.md

Guidance for people and coding agents working on hybrid-cloud-dashboard.

hybrid-cloud-dashboard is a hybrid, multi-cloud dashboard. One shared UI runs on three backends: Go on GCP Cloud Run, Python on AWS Lambda, and .NET on Azure App Service. A cross-cloud facade routes traffic between them, the `hcd-sync` service keeps data in sync, and the infrastructure is written in Terraform (not OpenTofu). Local testing runs on a kind cluster named `hcd` with mock clouds.

## Standard workflow for any new work

1. **Issue first.** Create a GitHub issue describing the work before any code changes.
2. **Link the wiki.** If the issue relates to an existing wiki page, update that wiki page to reference the new issue (link it, for example `#123`).
3. **Implement against the issue.** Do the work (via a coding agent) on a branch, and open a PR that links the issue (for example `Closes #123`). Until the integration branch `cursor/hybrid-cloud-v2-4a4d` is merged into `main`, branch from it and target PRs at it. A PR into a branch other than the default branch does not close its issue automatically, so close the issue by hand after the merge.
4. **Wait for CI.** Wait for the `ci` workflow to pass. This repo has no `validate` job. The `ci` workflow (`.github/workflows/ci.yml`) runs on every push and on pull requests into `main`, `cursor/hybrid-cloud-v2-4a4d`, or any `cursor/**` branch. It has two jobs:
   - `test` runs `make test`. That covers Go tests for `libs/merge`, the facade, `backends/go-gcp` and `sync`; pytest for `backends/python-aws` and the local seed script; `dotnet test` for the .NET backend; `node --test` for the frontend; and the static contract check.
   - `terraform` runs `make terraform-validate`. That is `terraform init -backend=false -lockfile=readonly` and `terraform validate` on each Terraform root, using Terraform 1.9.8, with no apply.
   - CI runs no separate lint/format job and no smoke/integration test. Run the end-to-end smoke check yourself on the local `hcd` kind cluster (`make cluster-up`, `tilt up`, `make local-seed`).
5. **Squash-merge.** Once both `ci` jobs pass, squash-merge the PR (`gh pr merge --squash`). Do not rely on GitHub auto-merge: if the repo has no required checks, auto-merge would merge immediately.
   - **Merge checklist:** Refuse to merge a pull request whose linked issue body is still a placeholder. Flesh out the issue and get the user's approval first (see "Issue as source of truth").
6. **Update the wiki with the fix.** Once the issue is fixed (merged), update the related wiki page again with screenshots of the fix, embedded in the wiki page itself. Commit the image files into the wiki repo (for example an `images/` folder in the `.wiki.git` repo) and render them inline with relative markdown image links (for example `![Fix](images/issue-123-after.png)`). Do not link to external URLs such as `raw.githubusercontent.com`.
7. **Deploy destroy check.** Right now no merge triggers a deploy. `release.yml` runs only by hand (`workflow_dispatch`) on `main`, and it is gated: it stops before any cloud call. If a merge to `main` ever triggers a deploy, check the Terraform `Plan:` line in that deploy after the merge. If the plan would destroy or replace any resource, cancel the deploy immediately and report back rather than letting it continue. A normal plan shows `0 to destroy`.

## Issue as source of truth

- **Acceptance criteria first.** Every coding issue must carry its own acceptance criteria before implementation begins: what "done" looks like, inputs and outputs, edge cases, and how the work gets verified.
- **No code on a placeholder.** If an issue body is just a placeholder, flesh it out first and wait for the user's approval before writing any code.
- **Where each record lives.** The issue is the source of truth for the work item, alongside the wiki and the build log. The wiki explains the system, the build log records what happened, and the issue defines what this piece of work must do.

## Related rules

- Never run `terraform apply`, whether locally or anywhere else. Terraform in this repo is validate-only for now.
- Do not deploy anything to the AWS, Azure, or GCP backends yet. Work is limited to local testing on the Mac (the `hcd` kind cluster with mock clouds) and PRs to this repo.
- Never commit real credentials to this public repo. Account IDs, tokens, and hostnames stay as placeholders.
- Give each distinct change its own branch and PR.
- When testing, post screenshots, and open a separate GitHub issue for each bug you find.
- Once the facade is deployed, `hybridcloud.vellamsetti.com` will be a GoDaddy CNAME pointing to it.

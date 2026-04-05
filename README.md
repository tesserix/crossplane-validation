<p align="center">
  <img src="https://img.shields.io/github/v/release/tesserix/crossplane-validation?style=flat-square&color=blue" alt="Release">
  <img src="https://img.shields.io/github/actions/workflow/status/tesserix/crossplane-validation/ci.yml?branch=main&style=flat-square&label=CI" alt="CI">
  <img src="https://img.shields.io/github/license/tesserix/crossplane-validation?style=flat-square" alt="License">
  <img src="https://img.shields.io/github/go-mod/go-version/tesserix/crossplane-validation?style=flat-square" alt="Go Version">
</p>

# crossplane-validate

**`terraform plan` for Crossplane.** Preview what your manifests will create, change, or destroy — before merging.

<p align="center">
  <code>crossplane</code> &middot; <code>kubernetes</code> &middot; <code>infrastructure-as-code</code> &middot; <code>gitops</code> &middot; <code>cloud-native</code> &middot; <code>terraform</code> &middot; <code>opentofu</code> &middot; <code>aws</code> &middot; <code>gcp</code> &middot; <code>azure</code> &middot; <code>devops</code> &middot; <code>ci-cd</code> &middot; <code>platform-engineering</code> &middot; <code>shift-left</code> &middot; <code>cli</code>
</p>

## About

`crossplane-validate` is an open-source CLI that brings `terraform plan`-style previews to [Crossplane](https://www.crossplane.io/). It shows you exactly what a PR will add, change, or destroy in your cloud infrastructure — before it gets merged.

It works by rendering Crossplane compositions, diffing the output between git branches, and optionally running a real Terraform/OpenTofu plan against your cloud. Sensitive fields are automatically masked, destructive changes are flagged, and the output can be posted as a PR comment for team review.

No cluster access required. Runs locally, in GitHub Actions, or any CI system.

```
$ crossplane-validate plan --manifests=./crossplane/

  ⚠ ATTENTION: High-impact changes detected
    ~ Instance/app-db: instanceClass changing (db.r6g.large → db.r6g.xlarge)
    ~ Cluster/prod-cluster: version upgrade (1.29 → 1.31)

═══ Structural Changes ═══

  - Password/nonprod-password
      - spec.forProvider.displayName: (sensitive value removed)

  ~ Instance/app-db
      ~ spec.forProvider.instanceClass: db.r6g.large → db.r6g.xlarge
      ~ spec.forProvider.allocatedStorage: 100 → 200
      + spec.forProvider.storageEncrypted: true
      + spec.forProvider.multiAz: true

  ~ Cluster/prod-cluster
      ~ spec.forProvider.version: 1.29 → 1.31

  + GlobalAddress/api-gateway-ip
      + spec.forProvider.addressType: EXTERNAL
      + spec.forProvider.project: my-gcp-project

Plan: 1 to add, 2 to change, 1 to destroy
```

Runs locally, in CI, or both. With the **in-cluster operator**, get live state comparison, drift detection, and real-time plans before merging.

---

<details>
<summary><strong>Table of Contents</strong></summary>

- [Why](#why)
- [Install](#install)
- [Commands](#commands) — `plan` | `validate` | `lint` | `scan` | `render` | `diff` | `status` | `drift`
- [Operator (Live Mode)](#operator-live-mode) — in-cluster state, real-time plans, drift detection
- [Guardrails](#guardrails) — sensitive masking, destructive warnings, CI gating
- [Cloud-Aware Plan](#cloud-aware-plan)
- [Multi-Cloud](#multi-cloud) — provider & credential auto-detection
- [GitHub Action](#github-action) — basic, OIDC, CI gate, linting
- [Configuration](#configuration) — config file, credential modes, OIDC
- [How It Works](#how-it-works) — architecture diagram
- [Composition Rendering](#composition-rendering)
- [Prerequisites](#prerequisites) — required, optional, cloud credentials
- [Project Structure](#project-structure)
- [Roadmap](#roadmap)

</details>

---

## Why

Crossplane manages cloud infrastructure through Kubernetes manifests. But unlike Terraform, there is no built-in way to preview what a change will do before it reaches your cloud.

A wrong value in a YAML field can resize a production database, delete a VPC, or expose a storage account to the internet. You find out after merging.

`crossplane-validate` catches these before the PR is merged.

---

## Install

**Homebrew**

```bash
brew install tesserix/tap/crossplane-validate
```

**Binary**

```bash
# macOS (Apple Silicon)
curl -sL https://github.com/tesserix/crossplane-validation/releases/latest/download/crossplane-validate-darwin-arm64 -o crossplane-validate
chmod +x crossplane-validate && sudo mv crossplane-validate /usr/local/bin/

# macOS (Intel)
curl -sL https://github.com/tesserix/crossplane-validation/releases/latest/download/crossplane-validate-darwin-amd64 -o crossplane-validate
chmod +x crossplane-validate && sudo mv crossplane-validate /usr/local/bin/

# Linux (amd64)
curl -sL https://github.com/tesserix/crossplane-validation/releases/latest/download/crossplane-validate-linux-amd64 -o crossplane-validate
chmod +x crossplane-validate && sudo mv crossplane-validate /usr/local/bin/

# Linux (arm64)
curl -sL https://github.com/tesserix/crossplane-validation/releases/latest/download/crossplane-validate-linux-arm64 -o crossplane-validate
chmod +x crossplane-validate && sudo mv crossplane-validate /usr/local/bin/

# Windows (PowerShell)
Invoke-WebRequest -Uri https://github.com/tesserix/crossplane-validation/releases/latest/download/crossplane-validate-windows-amd64.exe -OutFile crossplane-validate.exe
```

**From source**

```bash
go install github.com/tesserix/crossplane-validation/cmd/crossplane-validate@latest
```

---

## Commands

### `plan` — preview what a PR will change

Compares rendered manifests between two git refs and produces a field-level diff.

```bash
crossplane-validate plan --manifests=./crossplane/
crossplane-validate plan --base=main --target=feature/new-infra --manifests=./crossplane/
crossplane-validate plan --manifests=./crossplane/ --output=markdown   # for PR comments
crossplane-validate plan --manifests=./crossplane/ --output=json       # for automation
```

The plan renders compositions, so you see the actual managed resources that Crossplane would create — not just the raw claim YAML.

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--base` | `main` | Base branch to compare against |
| `--target` | `HEAD` | Target ref to validate |
| `--manifests`, `-m` | config | Path to manifest directory |
| `--output`, `-o` | `terminal` | Output format: `terminal`, `markdown`, `json` |
| `--cloud` | `false` | Enable cloud-aware plan via OpenTofu |
| `--detailed-exitcode` | `false` | Exit `0`=no changes, `1`=error, `2`=changes detected |
| `--show-sensitive` | `false` | Show sensitive values in plain text |
| `--config`, `-c` | `.crossplane-validate.yml` | Config file path |

### `validate` — check manifests against XRD schemas

Validates Claims and Composite Resources against their XRD schemas without needing a cluster.

```bash
crossplane-validate validate ./crossplane/
```

```
Errors:
  ✗ XBucket/my-bucket spec.bucketName: required field missing
  ✗ XBucket/my-bucket spec.acl: value "public" not in enum [private, public-read]

Warnings:
  ⚠ Role/app-role spec.providerConfigRef.name: ProviderConfig "default" not found in scanned manifests
  ⚠ XBucket/my-bucket: no matching Composition found for storage.example.org/v1alpha1 XBucket

Total: 2 errors, 2 warnings
```

**What it checks:**
- Required fields defined in XRD `openAPIV3Schema`
- Enum constraint violations
- Type mismatches (string where integer expected, etc.)
- ProviderConfig references exist in scanned manifests
- Composition references match XR types

### `lint` — run external validation tools

Wraps popular open-source linting tools with a single command. Auto-detects what is installed.

```bash
crossplane-validate lint ./crossplane/
crossplane-validate lint --tools=yamllint,kubeconform ./crossplane/
crossplane-validate lint --output=json ./crossplane/
```

```
Tool detection:
  yamllint               YAML syntax and style validation (available)
  kubeconform            Kubernetes manifest schema validation (available)
  pluto                  Deprecated Kubernetes API version detection (available)
  kube-linter            Kubernetes best practices and security analysis (not found)
  crossplane-validate    Crossplane composition and XRD validation (available)

Tools run: yamllint, kubeconform, pluto, crossplane-validate

kubeconform (2 issues)
  ✗ xrd.yaml CompositeResourceDefinition/xbuckets: invalid input

pluto (1 issue)
  ⚠ claim.yaml: API networking.k8s.io/v1beta1 Ingress/app deprecated in 1.19, removed in 1.22 — use networking.k8s.io/v1 instead

Total: 1 error, 1 warning
```

**Supported tools:**

| Tool | Purpose | Install |
|------|---------|---------|
| [yamllint](https://github.com/adrienverge/yamllint) | YAML syntax and style | `brew install yamllint` |
| [kubeconform](https://github.com/yannh/kubeconform) | K8s manifest schema validation | `brew install kubeconform` |
| [pluto](https://github.com/FairwindsOps/pluto) | Deprecated API detection | `brew install FairwindsOps/tap/pluto` |
| [kube-linter](https://github.com/stackrox/kube-linter) | K8s security and best practices | `brew install kube-linter` |
| [crossplane CLI](https://docs.crossplane.io/latest/cli/) | Crossplane XRD/composition validation | `brew install crossplane-cli` |

### `scan` — discover resources

```bash
crossplane-validate scan ./crossplane/
```

Recursively finds all Crossplane resources, follows `kustomization.yaml` paths, and auto-discovers subdirectories when kustomize only references non-Crossplane resources (e.g., ArgoCD ApplicationSets).

### `render` — preview composed resources

```bash
crossplane-validate render ./crossplane/
crossplane-validate render ./crossplane/ --functions=./functions/
```

Renders compositions into the managed resources Crossplane would create. Uses `crossplane render` (Docker) when available for full Function Pipeline support, falls back to built-in Patch-and-Transform rendering otherwise.

### `diff` — quick structural comparison

```bash
crossplane-validate diff ./crossplane/
```

### `status` — live cluster resource health (requires operator)

```bash
crossplane-validate status
crossplane-validate status --context=prod-cluster
```

Connects to the in-cluster operator and shows Crossplane resource counts, readiness, and conditions.

### `drift` — git vs cluster differences (requires operator)

```bash
crossplane-validate drift --manifests=./crossplane/
crossplane-validate drift --manifests=./crossplane/ --context=prod-cluster
```

Compares your git manifests against what's actually running in the cluster. Shows resources missing in cluster, extra resources not in git, and spec field drift.

---

## Operator (Live Mode)

The operator runs inside your Kubernetes cluster alongside Crossplane. It watches all Crossplane resources, caches their live state, and exposes a gRPC API that the CLI connects to.

This enables **live plans** — instead of just comparing git branches, you see what will change relative to what's *actually deployed*.

```
$ crossplane-validate plan --live --manifests=./crossplane/

Connected to operator | Resources cached: 47 | Cache age: 12s

⚠ DRIFT DETECTED
  ~ Instance/app-db: field allocatedStorage differs (cluster: 150, proposed: 200)

═══ Proposed Changes ═══

  ~ Instance/app-db (Ready)
      ~ spec.forProvider.instanceClass: db.r6g.large → db.r6g.xlarge
      ~ spec.forProvider.allocatedStorage: 150 (live) → 200

  + GlobalAddress/api-gateway-ip
      + spec.forProvider.addressType: EXTERNAL

Plan: 1 to add, 1 to change, 0 to destroy
Drift: 1 resource drifted from git
```

### Install the Operator

**Helm**

```bash
helm install crossplane-validate-operator \
  oci://ghcr.io/tesserix/charts/crossplane-validate-operator \
  --namespace crossplane-system
```

Or from source:

```bash
helm install crossplane-validate-operator \
  deploy/helm/crossplane-validate-operator \
  --namespace crossplane-system --create-namespace
```

**Kustomize**

```bash
kubectl apply -k deploy/kustomize/base
```

### Operator Architecture

The operator is a single pod with:
- **Dynamic informers** watching all Crossplane CRDs (auto-discovers installed providers)
- **gRPC API** on port 9443 for CLI connectivity
- **Health endpoint** on port 8081 for Kubernetes probes
- **Read-only RBAC** — only `get`, `list`, `watch` on Crossplane resources

The operator image is public at `ghcr.io/tesserix/crossplane-validate-operator`.

### Live Plan Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--live` | `false` | Connect to in-cluster operator for live state comparison |
| `--operator-address` | `""` | Direct gRPC address (default: auto port-forward) |
| `--context` | `""` | Kubernetes context to use |
| `--namespace` | `crossplane-system` | Operator namespace |

### Notifications

The operator can post plan summaries to Slack or Microsoft Teams:

```bash
helm install crossplane-validate-operator deploy/helm/crossplane-validate-operator \
  --set notifications.slack.enabled=true \
  --set notifications.slack.webhookUrl=https://hooks.slack.com/services/xxx
```

### Live Plan in GitHub Actions

```yaml
steps:
  - uses: actions/checkout@v4
    with:
      fetch-depth: 0

  # Auth to your cluster (OIDC recommended)
  # ...

  - name: Port-forward to operator
    run: |
      kubectl port-forward -n crossplane-system svc/crossplane-validate-operator 9443:9443 &
      sleep 3

  - name: Live plan
    run: |
      crossplane-validate plan \
        --live --operator-address=localhost:9443 \
        --manifests=./crossplane/ --output=markdown > plan.md

  - name: Comment on PR
    uses: actions/github-script@v7
    with:
      script: |
        const fs = require('fs');
        const body = fs.readFileSync('plan.md', 'utf8');
        const { data: comments } = await github.rest.issues.listComments({
          owner: context.repo.owner, repo: context.repo.repo,
          issue_number: context.issue.number,
        });
        const existing = comments.find(c => c.body.includes('### Crossplane Validation'));
        if (existing) {
          await github.rest.issues.updateComment({
            owner: context.repo.owner, repo: context.repo.repo,
            comment_id: existing.id, body,
          });
        } else {
          await github.rest.issues.createComment({
            owner: context.repo.owner, repo: context.repo.repo,
            issue_number: context.issue.number, body,
          });
        }
```

---

## Guardrails

### Sensitive field masking

Passwords, tokens, API keys, certificates, and connection strings are automatically redacted in all output formats — including PR comments.

```
  ~ Instance/app-db
      ~ spec.forProvider.masterPassword: (sensitive value) → (sensitive value changed)
```

Use `--show-sensitive` to see raw values when running locally.

### Destructive change warnings

Deletions and high-risk field changes are called out before the diff:

```
  ⚠ WARNING: 2 resources will be DESTROYED
    - Instance/app-db (rds.aws.upbound.io/v1beta1)
    - VPC/deprecated-vpc (ec2.aws.upbound.io/v1beta1)

  ⚠ ATTENTION: High-impact changes detected
    ~ LinuxVirtualMachine/app-vm: size changing (Standard_B2s → Standard_D4s_v3)
    ~ Cluster/prod-cluster: version upgrade (1.29 → 1.31)
    ~ VPC/main-vpc: cidrBlock changing (10.0.0.0/16 → 10.0.0.0/12)
```

High-risk fields: `instanceClass`, `instanceType`, `size`, `vmSize`, `engine`, `engineVersion`, `version`, `cidrBlock`, `cidrRange`, `deletionPolicy`, `publiclyAccessible`.

### CI gating with exit codes

```bash
crossplane-validate plan --manifests=./crossplane/ --detailed-exitcode
# Exit 0 = no changes
# Exit 1 = error
# Exit 2 = changes detected (use to require approval)
```

---

## Cloud-Aware Plan

The `--cloud` flag goes beyond structural diff. It converts your Crossplane resources to HCL, queries the actual Terraform provider schema at runtime, imports existing cloud resources, and runs a real `terraform plan`:

```bash
crossplane-validate plan --manifests=./crossplane/ --cloud
```

```
Cloud authentication:
  aws:         profile "default" (env)
  azure:       OIDC federation (env)

Loading provider schemas...
Converting to HCL...
Running cloud plan (read-only)...

═══ Structural Changes ═══

  ~ Account/storage01
      ~ spec.forProvider.accountReplicationType: LRS → GRS

═══ Cloud Impact ═══

  ~ azurerm_storage_account.storage01
      ~ account_replication_type: "LRS" -> "GRS"
      ~ tags: {Environment:devtest} → {Environment:production}

Plan: 0 to add, 1 to change, 0 to destroy
Cloud: 0 to add, 1 to change, 0 to destroy
```

If no credentials are found, the CLI tells you exactly what it checked:

```
Cloud mode: no credentials detected
  Checked: AWS_ACCESS_KEY_ID, AWS_PROFILE, GOOGLE_APPLICATION_CREDENTIALS,
           ARM_CLIENT_ID, ARM_SUBSCRIPTION_ID, OIDC tokens
  Tip: authenticate with your cloud provider or set credentials in .crossplane-validate.yml
  Skipping cloud plan.
```

**How it works:**
1. Auto-detects providers from your manifests (AWS, GCP, Azure, Azure AD, Datadog, any Upjet provider)
2. Auto-detects credentials from your environment (env vars, CLI profiles, OIDC, managed identity)
3. Downloads provider schema via `terraform providers schema -json` — no hardcoded resource mappings
4. Dynamically maps Crossplane resource types to Terraform types
5. Imports existing resources via `terraform import` using external-name annotations
6. Runs `terraform plan` to show real cloud impact

**Requirements:** Terraform or OpenTofu installed, cloud credentials available (read-only recommended).

---

## Multi-Cloud

### Provider Auto-Detection

Providers are detected automatically from your manifests. No configuration needed.

| Cloud | Detected from | Terraform Provider |
|-------|---------------|-------------------|
| AWS | `*.aws.upbound.io` | `hashicorp/aws` |
| GCP | `*.gcp.upbound.io` | `hashicorp/google` |
| Azure | `*.azure.upbound.io` | `hashicorp/azurerm` |
| Azure AD | `*.azuread.upbound.io` | `hashicorp/azuread` |
| Datadog | `*.datadog.upbound.io` | `datadog/datadog` |
| Any Upjet | `*.<name>.upbound.io` | Derived dynamically |

### Credential Auto-Detection

Credentials are also auto-detected from your environment. The CLI checks standard auth methods in order:

| Provider | Auth Method | How It's Detected |
|----------|-------------|-------------------|
| **AWS** | Access keys | `AWS_ACCESS_KEY_ID` + `AWS_SECRET_ACCESS_KEY` |
| | Named profile | `AWS_PROFILE` (uses `~/.aws/credentials`) |
| | OIDC federation | `AWS_WEB_IDENTITY_TOKEN_FILE` + `AWS_ROLE_ARN` |
| | Instance profile | EC2/ECS metadata (automatic) |
| **GCP** | Service account key | `GOOGLE_APPLICATION_CREDENTIALS` |
| | Application Default Credentials | `gcloud auth application-default login` |
| | Workload Identity | GCP auth GitHub Action sets `GOOGLE_APPLICATION_CREDENTIALS` |
| | Service account impersonation | Config: `service-account` field |
| **Azure** | Service principal | `ARM_CLIENT_ID` + `ARM_CLIENT_SECRET` + `ARM_TENANT_ID` |
| | Azure CLI | `az login` (detected via `ARM_USE_CLI`) |
| | OIDC federation | `ARM_USE_OIDC=true` (auto-detected in GitHub Actions) |
| | Managed Identity | `ARM_USE_MSI=true` (for Azure-hosted runners) |

In GitHub Actions, OIDC is auto-detected when `ACTIONS_ID_TOKEN_REQUEST_URL` is set — no manual configuration needed.

---

## GitHub Action

### Basic (PR comment with plan)

```yaml
name: Crossplane Validate
on:
  pull_request:
    paths: ['crossplane/**']

permissions:
  pull-requests: write
  contents: read

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - name: Install
        run: |
          curl -sL https://github.com/tesserix/crossplane-validation/releases/latest/download/crossplane-validate-linux-amd64 -o crossplane-validate
          chmod +x crossplane-validate

      - name: Plan
        run: ./crossplane-validate plan --base=origin/main --target=HEAD --manifests=./crossplane/ --output=markdown > plan.md 2>/dev/null || true

      - name: Comment
        uses: actions/github-script@v7
        with:
          script: |
            const fs = require('fs');
            const body = fs.readFileSync('plan.md', 'utf8') || '### Crossplane Validation\n\nNo changes detected.';
            const { data: comments } = await github.rest.issues.listComments({
              owner: context.repo.owner, repo: context.repo.repo,
              issue_number: context.issue.number,
            });
            const existing = comments.find(c => c.body.includes('### Crossplane Validation'));
            if (existing) {
              await github.rest.issues.updateComment({
                owner: context.repo.owner, repo: context.repo.repo,
                comment_id: existing.id, body,
              });
            } else {
              await github.rest.issues.createComment({
                owner: context.repo.owner, repo: context.repo.repo,
                issue_number: context.issue.number, body,
              });
            }
```

### With cloud plan (OIDC — no secrets needed)

```yaml
      - name: Auth to AWS (OIDC)
        uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: arn:aws:iam::123456789012:role/crossplane-validate
          aws-region: us-east-1

      - name: Cloud Plan
        run: |
          ./crossplane-validate plan \
            --base=origin/main --target=HEAD \
            --manifests=./crossplane/ \
            --cloud --output=markdown > plan.md 2>/dev/null || true
```

```yaml
      # Azure OIDC
      - name: Auth to Azure (OIDC)
        uses: azure/login@v2
        with:
          client-id: ${{ secrets.AZURE_CLIENT_ID }}
          tenant-id: ${{ secrets.AZURE_TENANT_ID }}
          subscription-id: ${{ secrets.AZURE_SUBSCRIPTION_ID }}

      # GCP Workload Identity
      - name: Auth to GCP (OIDC)
        uses: google-github-actions/auth@v2
        with:
          workload_identity_provider: projects/123/locations/global/workloadIdentityPools/github/providers/repo
          service_account: validate@my-project.iam.gserviceaccount.com
```

### With CI gate (block merge on changes without approval)

```yaml
      - name: Plan with exit code
        run: |
          ./crossplane-validate plan \
            --base=origin/main --target=HEAD \
            --manifests=./crossplane/ \
            --detailed-exitcode
          # Exit 0 = no changes, 1 = error, 2 = changes detected
```

### With validation and linting

```yaml
      - name: Validate schemas
        run: ./crossplane-validate validate ./crossplane/

      - name: Lint
        run: |
          pip install yamllint
          ./crossplane-validate lint ./crossplane/
```

---

## Configuration

Optional `.crossplane-validate.yml` in your repo root. Without it, the CLI scans the current directory and auto-detects everything.

### Minimal (most common)

```yaml
manifests:
  - crossplane/
```

### With provider regions/projects

```yaml
manifests:
  - crossplane/
  - infra/cloud/

providers:
  aws:
    region: us-east-1
  gcp:
    project: my-gcp-project
  azure:
    subscription-id: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
    tenant-id: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
```

### With OIDC federation (CI/CD)

```yaml
providers:
  aws:
    credentials: oidc
    region: us-east-1
    role-arn: arn:aws:iam::123456789012:role/crossplane-validate-readonly

  gcp:
    credentials: oidc
    project: my-gcp-project
    service-account: validate@my-gcp-project.iam.gserviceaccount.com

  azure:
    credentials: oidc
    subscription-id: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
    tenant-id: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
    client-id: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
```

### Credential modes

| Mode | Value | Description |
|------|-------|-------------|
| Auto-detect | `""` (default) | Checks environment variables, CLI profiles, OIDC, managed identity |
| Environment | `env` | Uses standard cloud SDK environment variables |
| Default chain | `default` | AWS profiles, GCP ADC, Azure CLI |
| OIDC | `oidc` | Workload identity federation (GitHub Actions, GitLab CI) |
| Azure CLI | `cli` | Uses `az login` session |
| Managed Identity | `msi` | Azure VMs, AKS, GitHub-hosted runners with MSI |

### Settings

```yaml
settings:
  timeout: 10m
  ignore-fields:
    - metadata.resourceVersion
    - metadata.uid
    - status.conditions
```

---

## How It Works

```
                    Crossplane Manifests (YAML)
                              │
                    ┌─────────┴──────────┐
                    │  Scanner           │
                    │  Follows kustomize │
                    │  Classifies types  │
                    └─────────┬──────────┘
                              │
                    ┌─────────┴──────────┐
                    │  Validator         │
                    │  XRD schemas       │
                    │  ProviderConfigs   │
                    └─────────┬──────────┘
                              │
                    ┌─────────┴──────────┐
                    │  Renderer          │
                    │  Compositions →    │
                    │  Managed Resources │
                    └─────────┬──────────┘
                              │
              ┌───────────────┼───────────────┐
              │                               │
    ┌─────────┴──────────┐         ┌──────────┴──────────┐
    │  Structural Diff   │         │  Cloud Plan         │
    │  (always)          │         │  (--cloud only)     │
    │                    │         │                     │
    │  base ↔ target     │         │  HCL conversion     │
    │  field-level diff  │         │  provider schema    │
    │  sensitive masking │         │  terraform plan     │
    └─────────┬──────────┘         └──────────┬──────────┘
              └────────────┬──────────────────┘
                           │
                 ┌─────────┴──────────┐
                 │  Output            │
                 │  terminal / md /   │
                 │  json              │
                 └────────────────────┘
```

**Default mode** (no flags): git-based structural diff. Renders compositions from both branches, diffs the output. Fast, offline, no credentials needed.

**With `--cloud`**: converts to HCL, downloads provider schemas at runtime, imports existing resources, runs real Terraform plan against your cloud.

---

## Composition Rendering

The renderer handles both composition types:

**Patch-and-Transform** — built-in, no dependencies. Extracts base resources, applies field patches from XR spec to managed resource spec.

**Function Pipeline** — uses `crossplane render` when Docker and the Crossplane CLI are available. Supports go-templating and any composition function. Falls back to built-in rendering if Docker is not running.

Nested compositions (XR → Composition → XR → Composition) are followed up to 5 levels deep.

---

## Prerequisites

### Required

| Tool | Purpose | Install |
|------|---------|---------|
| Git | Branch comparison for `plan` and `diff` | Pre-installed on most systems |

### Optional

| Tool | Purpose | When | Install |
|------|---------|------|---------|
| Terraform or OpenTofu | Cloud-aware plan | `--cloud` flag | `brew install opentofu` or `brew install terraform` |
| Docker | Function Pipeline rendering | Compositions with functions | [docker.com](https://docs.docker.com/get-docker/) |
| Crossplane CLI | Full composition rendering | Compositions with functions | `brew install crossplane-cli` |
| yamllint | YAML syntax checking | `lint` command | `brew install yamllint` |
| kubeconform | K8s schema validation | `lint` command | `brew install kubeconform` |
| pluto | Deprecated API detection | `lint` command | `brew install FairwindsOps/tap/pluto` |
| kube-linter | K8s best practices | `lint` command | `brew install kube-linter` |

### Cloud credentials (for `--cloud` mode)

No special setup needed — the CLI auto-detects credentials from your environment:

| If you use... | Just do this | It works because... |
|---------------|--------------|---------------------|
| AWS CLI | `aws configure` or `export AWS_PROFILE=myprofile` | Reads `~/.aws/credentials` |
| GCP CLI | `gcloud auth application-default login` | Sets Application Default Credentials |
| Azure CLI | `az login` | Terraform detects Azure CLI session |
| GitHub Actions | Add OIDC auth step to workflow | Auto-detected from `ACTIONS_ID_TOKEN_REQUEST_URL` |
| Service accounts | Set `GOOGLE_APPLICATION_CREDENTIALS` or `ARM_CLIENT_ID` | Standard SDK environment variables |
| Managed Identity | Run on Azure VM/AKS/GitHub-hosted runners with MSI | Set `ARM_USE_MSI=true` |

Read-only credentials are recommended. The CLI only reads cloud state — it never creates, modifies, or deletes resources.

---

## Project Structure

```
crossplane-validation/
├── cmd/
│   ├── crossplane-validate/          CLI (plan, validate, lint, scan, render, diff, status, drift)
│   └── crossplane-validate-operator/ In-cluster operator binary
├── api/proto/v1/                     gRPC service definition (protobuf)
├── pkg/
│   ├── manifest/                     YAML scanning, kustomize discovery, resource classification
│   ├── validate/                     XRD schema validation, ProviderConfig/Composition checks
│   ├── renderer/                     Composition rendering (built-in + crossplane render)
│   ├── diff/                         Structural diff engine, sensitive masking, array diffs
│   ├── hcl/                          Crossplane → HCL conversion, dynamic schema lookup
│   ├── tofu/                         Terraform/OpenTofu plan execution
│   ├── plan/                         Output rendering (terminal, markdown, JSON)
│   ├── lint/                         External tool wrapper (yamllint, kubeconform, pluto, etc.)
│   ├── config/                       Configuration file loading
│   ├── operator/                     State cache, live plan computation, drift detection
│   ├── grpc/                         gRPC server and client
│   ├── k8s/                          Dynamic CRD discovery, port-forward helper
│   └── notify/                       Slack and Teams notification senders
├── deploy/
│   ├── helm/crossplane-validate-operator/  Helm chart for operator
│   └── kustomize/                          Kustomize manifests for operator
├── testdata/                         Test manifests (AWS, GCP, Azure, kustomize, compositions)
├── Dockerfile                        Multi-stage build for operator image
├── Makefile                          Build, test, docker, helm targets
├── .github/workflows/                CI, Release, PR Validation, Live Validation
├── action.yml                        GitHub Action definition
└── ROADMAP.md                        Planned features
```

---

## Roadmap

See [ROADMAP.md](ROADMAP.md) for planned features:

- Cost estimation for infrastructure changes
- Drift detection (cluster state vs git state)
- Policy engine (OPA/Rego integration)
- ArgoCD pre-sync hook integration
- Additional provider support (Cloudflare, MongoDB Atlas, Vault, etc.)
- Docker image and Krew plugin

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Good first contributions:

- Improve composition rendering for edge cases
- Add test coverage for additional resource types
- Documentation and examples

---

## Security

- **Sensitive field masking** — passwords, tokens, API keys, certificates automatically redacted in all output (PR comments, terminal, JSON). Use `--show-sensitive` to override locally.
- **Read-only credentials** — the CLI only reads cloud state, never creates or modifies resources. Use read-only IAM roles/policies.
- **OIDC preferred** — use workload identity federation instead of long-lived secrets in CI/CD. No secrets to rotate or leak.
- **No credentials in output** — cloud credentials are never printed in plan output, logs, or PR comments.
- **Never commit credentials** — use environment variables, CLI auth, or OIDC. The `.crossplane-validate.yml` config file stores references (`oidc`, `env`, `cli`), not actual secrets.
- Report vulnerabilities to unidevidp@gmail.com (see [SECURITY.md](SECURITY.md))

---

## License

Apache License 2.0 — see [LICENSE](LICENSE).

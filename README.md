<p align="center">
  <img src="https://img.shields.io/github/v/release/tesserix/crossplane-validation?style=flat-square&color=blue" alt="Release">
  <img src="https://img.shields.io/github/actions/workflow/status/tesserix/crossplane-validation/ci.yml?branch=main&style=flat-square&label=CI" alt="CI">
  <img src="https://img.shields.io/github/license/tesserix/crossplane-validation?style=flat-square" alt="License">
  <img src="https://img.shields.io/github/go-mod/go-version/tesserix/crossplane-validation?style=flat-square" alt="Go Version">
</p>

# crossplane-validate

**`terraform plan` for Crossplane.** Preview what your manifests will create, change, or destroy — before merging.

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

Runs locally, in CI, or both. No cluster required.

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

**How it works:**
1. Auto-detects providers from your manifests (AWS, GCP, Azure, Azure AD, Datadog, any Upjet provider)
2. Downloads provider schema via `terraform providers schema -json` — no hardcoded resource mappings
3. Dynamically maps Crossplane resource types to Terraform types
4. Imports existing resources via `terraform import` using external-name annotations
5. Runs `terraform plan` to show real cloud impact

**Requirements:** Terraform or OpenTofu installed, cloud credentials in environment (read-only recommended).

---

## Multi-Cloud

Providers are auto-detected from your manifests. No configuration needed.

| Cloud | Detected from | Terraform Provider |
|-------|---------------|-------------------|
| AWS | `*.aws.upbound.io` | `hashicorp/aws` |
| GCP | `*.gcp.upbound.io` | `hashicorp/google` |
| Azure | `*.azure.upbound.io` | `hashicorp/azurerm` |
| Azure AD | `*.azuread.upbound.io` | `hashicorp/azuread` |
| Datadog | `*.datadog.upbound.io` | `datadog/datadog` |
| Any Upjet | `*.<name>.upbound.io` | Derived dynamically |

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

### With CI gate (block merge on changes without approval)

```yaml
      - name: Plan with exit code
        run: |
          ./crossplane-validate plan \
            --base=origin/main --target=HEAD \
            --manifests=./crossplane/ \
            --detailed-exitcode
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

Optional `.crossplane-validate.yml` in your repo root:

```yaml
manifests:
  - crossplane/
  - infra/cloud/

providers:
  aws:
    credentials: env
    region: us-east-1
  gcp:
    credentials: env
    project: my-project
  azure:
    credentials: env
    subscription-id: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx

settings:
  timeout: 10m
  ignore-fields:
    - metadata.resourceVersion
    - metadata.uid
    - status.conditions
```

Without a config file, the CLI scans the current directory and auto-detects providers.

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

| Requirement | When |
|---|---|
| Git | Always (branch comparison) |
| Terraform or OpenTofu | `--cloud` mode only |
| Cloud credentials (read-only) | `--cloud` mode only |
| Docker + Crossplane CLI | Full Function Pipeline rendering (optional) |

For basic validation (`plan`, `validate`, `scan`), all you need is the binary and a git repo.

---

## Project Structure

```
crossplane-validation/
├── cmd/crossplane-validate/     CLI entry point (plan, validate, lint, scan, render, diff)
├── pkg/
│   ├── manifest/                YAML scanning, kustomize discovery, resource classification
│   ├── validate/                XRD schema validation, ProviderConfig/Composition checks
│   ├── renderer/                Composition rendering (built-in + crossplane render)
│   ├── diff/                    Structural diff engine, sensitive masking, array diffs
│   ├── hcl/                     Crossplane → HCL conversion, dynamic schema lookup
│   ├── tofu/                    Terraform/OpenTofu plan execution
│   ├── plan/                    Output rendering (terminal, markdown, JSON)
│   ├── lint/                    External tool wrapper (yamllint, kubeconform, pluto, etc.)
│   └── config/                  Configuration file loading
├── testdata/                    Test manifests (AWS, GCP, Azure, kustomize, compositions)
├── .github/workflows/           CI, Release, PR Validation
├── action.yml                   GitHub Action definition
└── ROADMAP.md                   Planned features
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

- Sensitive fields are automatically masked in all output
- Use read-only cloud credentials for `--cloud` mode
- Never commit credentials to your repository
- Report vulnerabilities to unidevidp@gmail.com (see [SECURITY.md](SECURITY.md))

---

## License

Apache License 2.0 — see [LICENSE](LICENSE).

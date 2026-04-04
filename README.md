<p align="center">
  <img src="https://img.shields.io/github/v/release/tesserix/crossplane-validation?style=flat-square&color=blue" alt="Release">
  <img src="https://img.shields.io/github/actions/workflow/status/tesserix/crossplane-validation/ci.yml?branch=main&style=flat-square&label=CI" alt="CI">
  <img src="https://img.shields.io/github/license/tesserix/crossplane-validation?style=flat-square" alt="License">
  <img src="https://img.shields.io/github/go-mod/go-version/tesserix/crossplane-validation?style=flat-square" alt="Go Version">
  <img src="https://img.shields.io/badge/crossplane-compatible-blueviolet?style=flat-square" alt="Crossplane Compatible">
  <img src="https://img.shields.io/badge/multi--cloud-AWS%20%7C%20GCP%20%7C%20Azure-orange?style=flat-square" alt="Multi-Cloud">
</p>

# crossplane-validate

**`terraform plan` for Crossplane.** See exactly what your Crossplane manifests will create, change, or destroy — before you apply.

---

## The Problem

Crossplane is powerful for managing cloud infrastructure as Kubernetes resources. But unlike Terraform, there's no built-in way to preview changes before they hit your cloud. A single misconfigured manifest can create unintended resources, modify production infrastructure, or rack up costs — and you only find out after it's applied.

Teams using Crossplane today are merging PRs blind, hoping the YAML is correct.

## The Solution

`crossplane-validate` bridges this gap. It reads your Crossplane manifests, renders compositions, and produces a clear, field-level diff showing what will happen — just like `terraform plan`.

```
$ crossplane-validate plan --manifests=./crossplane/

═══ Structural Changes ═══

  + Bucket/logs-bucket
      + spec.forProvider.region: us-east-1
      + spec.forProvider.acl: private

  ~ Role/app-role
      ~ spec.forProvider.tags.Environment: staging → production

  - VPC/deprecated-vpc

Plan: 1 to add, 1 to change, 1 to destroy
```

It runs locally on your machine, in CI on every PR, or both. No cluster required for basic validation.

---

## Features

| Feature | Description |
|---|---|
| **Git-based diff** | Compares manifests between branches — shows what a PR will change |
| **Cloud-aware plan** | Converts to HCL and runs Terraform/OpenTofu for real cloud-state diff |
| **Dynamic provider schema** | Queries Terraform provider schema at runtime — no hardcoded resource mappings |
| **Multi-cloud** | AWS, GCP, Azure, Azure AD, Datadog — auto-detected from your manifests |
| **Composition rendering** | Renders Patch-and-Transform and Function Pipeline compositions offline |
| **Resource discovery** | Recursive scan of any directory to find all Crossplane resources |
| **PR comments** | Markdown output designed for GitHub PR comments |
| **Zero dependencies** | Single static binary. No Docker, no cluster, no Helm |
| **Homebrew** | `brew install tesserix/tap/crossplane-validate` |
| **GitHub Action** | Drop-in action for any repo |

---

## Quick Start

### Install

**Homebrew (macOS / Linux)**

```bash
brew tap tesserix/tap
brew install crossplane-validate
```

**Direct download**

```bash
# macOS (Apple Silicon)
curl -sL https://github.com/tesserix/crossplane-validation/releases/latest/download/crossplane-validate-darwin-arm64 -o crossplane-validate
chmod +x crossplane-validate
sudo mv crossplane-validate /usr/local/bin/

# macOS (Intel)
curl -sL https://github.com/tesserix/crossplane-validation/releases/latest/download/crossplane-validate-darwin-amd64 -o crossplane-validate
chmod +x crossplane-validate
sudo mv crossplane-validate /usr/local/bin/

# Linux (amd64)
curl -sL https://github.com/tesserix/crossplane-validation/releases/latest/download/crossplane-validate-linux-amd64 -o crossplane-validate
chmod +x crossplane-validate
sudo mv crossplane-validate /usr/local/bin/

# Linux (arm64)
curl -sL https://github.com/tesserix/crossplane-validation/releases/latest/download/crossplane-validate-linux-arm64 -o crossplane-validate
chmod +x crossplane-validate
sudo mv crossplane-validate /usr/local/bin/

# Windows (PowerShell)
Invoke-WebRequest -Uri https://github.com/tesserix/crossplane-validation/releases/latest/download/crossplane-validate-windows-amd64.exe -OutFile crossplane-validate.exe
```

**Build from source**

```bash
go install github.com/tesserix/crossplane-validation/cmd/crossplane-validate@latest
```

### Verify

```bash
crossplane-validate --version
```

---

## Usage

### Scan — discover Crossplane resources

Recursively find and classify all Crossplane resources in a directory:

```bash
crossplane-validate scan ./crossplane/
```

```
Scanned: ./crossplane/

XRDs (32)
  CompositeResourceDefinition/xsandboxes.platform.example.com  apiextensions.crossplane.io/v1
  CompositeResourceDefinition/xnetworks.platform.example.com   apiextensions.crossplane.io/v1
  ...

Compositions (32)
  Composition/xsandbox.composition.v1   apiextensions.crossplane.io/v1
  ...

Managed Resources (10)
  Application/platform-apps-prod        applications.azuread.upbound.io/v1beta1
  DashboardJSON/monitoring-dashboard    datadog.upbound.io/v1alpha1
  ...

Total: 184 resources
```

### Plan — see what will change

Compare your working directory against the `main` branch:

```bash
crossplane-validate plan --manifests=./crossplane/
```

Compare specific branches:

```bash
crossplane-validate plan --base=main --target=feature/add-storage --manifests=./crossplane/
```

Output as markdown (for PR comments):

```bash
crossplane-validate plan --manifests=./crossplane/ --output=markdown
```

### Render — see composed resources

Render compositions to see the managed resources Crossplane would create:

```bash
crossplane-validate render ./crossplane/
```

### Diff — quick structural comparison

```bash
crossplane-validate diff ./crossplane/
```

### Cloud-aware plan

For deeper validation, add the `--cloud` flag. This dynamically queries the Terraform provider schema, converts your Crossplane resources to HCL, imports existing cloud resources, and runs a real Terraform plan:

```bash
crossplane-validate plan --manifests=./crossplane/ --cloud
```

```
Loading provider schemas...        # downloads schema for detected providers
Converting to HCL...               # converts Crossplane MRs → Terraform resources
Importing azurerm_storage_account.myapp...  # imports existing resource from Azure
Import successful!

  ~ azurerm_storage_account.myapp will be updated in-place
      ~ account_replication_type: "LRS" -> "GRS"
      ~ allow_nested_items_to_be_public: true -> false
      ~ tags: {Environment:devtest} → {Environment:production, CostCenter:engineering}

Plan: 0 to add, 1 to change, 0 to destroy
Cloud: 0 to add, 1 to change, 0 to destroy
```

This requires Terraform or OpenTofu installed and cloud credentials available (see [Configuration](#configuration)).

---

## How It Works

```
                     Your Crossplane Manifests
                              │
                    ┌─────────┴─────────┐
                    │  Manifest Scanner  │
                    │  Parses YAMLs,     │
                    │  classifies types  │
                    └─────────┬─────────┘
                              │
                    ┌─────────┴─────────┐
                    │    Renderer        │
                    │  Renders comps,    │
                    │  applies patches   │
                    └─────────┬─────────┘
                              │
              ┌───────────────┼───────────────┐
              │                               │
    ┌─────────┴─────────┐          ┌─────────┴─────────┐
    │   Git Diff Engine │          │  HCL Converter     │
    │   (always runs)   │          │  (--cloud only)    │
    │                   │          │                    │
    │  main vs feature  │          │  Provider schema   │
    │  field-level diff │          │  → dynamic mapping │
    └─────────┬─────────┘          │  → terraform plan  │
              │                    └─────────┬─────────┘
              └───────────┬───────────────────┘
                          │
                ┌─────────┴─────────┐
                │   Plan Renderer   │
                │  terminal / md /  │
                │  json output      │
                └───────────────────┘
```

**By default**, the tool performs a git-based diff. It renders manifests from both branches and diffs the output. Fast, offline, no credentials needed.

**With `--cloud`**, it goes deeper:
1. Auto-detects which cloud providers are used from your manifests
2. Queries `terraform providers schema -json` to get the full resource type catalog (1,000+ types per provider)
3. Dynamically maps Crossplane kinds to Terraform resource types
4. Imports existing cloud resources into Terraform state
5. Runs `terraform plan` to show the real cloud impact with field-level diffs

---

## Multi-Cloud Support

Providers are **auto-detected** from your manifests. If your directory contains AWS, GCP, and Azure resources, all three providers are included automatically.

| Provider | Auto-detected from | Terraform provider |
|---|---|---|
| AWS | `*.aws.upbound.io` | `hashicorp/aws` |
| GCP | `*.gcp.upbound.io` | `hashicorp/google` |
| Azure | `*.azure.upbound.io` | `hashicorp/azurerm` |
| Azure AD | `*.azuread.upbound.io` | `hashicorp/azuread` |
| Datadog | `*.datadog.upbound.io` | `datadog/datadog` |
| Any Upjet provider | `*.<name>.upbound.io` | Derived dynamically |

Resource type mapping in `--cloud` mode is **fully dynamic** — it queries the actual Terraform provider schema at runtime instead of relying on hardcoded mappings. This means new resources added to providers work automatically without CLI updates.

---

## GitHub Action

Add this to your repo to get plan comments on every PR:

```yaml
# .github/workflows/crossplane-validate.yml
name: Crossplane Validate

on:
  pull_request:
    paths: ['crossplane/**']

jobs:
  validate:
    runs-on: ubuntu-latest
    permissions:
      pull-requests: write
      contents: read
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0

      - uses: tesserix/crossplane-validation@v0.7.0
        with:
          manifests: ./crossplane/
```

### Action inputs

| Input | Default | Description |
|---|---|---|
| `version` | `latest` | Version to install (e.g., `v0.7.0`) |
| `manifests` | `.` | Path to Crossplane manifests |
| `base-branch` | `main` | Branch to compare against |
| `cloud-mode` | `false` | Enable cloud-aware plan |
| `output-format` | `markdown` | `terminal`, `markdown`, or `json` |

### Full workflow with PR comments

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

      - name: Install crossplane-validate
        run: |
          curl -sL https://github.com/tesserix/crossplane-validation/releases/latest/download/crossplane-validate-linux-amd64 -o crossplane-validate
          chmod +x crossplane-validate

      - name: Run plan
        id: plan
        run: |
          ./crossplane-validate plan \
            --base=origin/main \
            --target=HEAD \
            --manifests=./crossplane/ \
            --output=markdown > plan.md 2>&1 || true

      - name: Comment on PR
        uses: actions/github-script@v7
        with:
          script: |
            const fs = require('fs');
            const body = fs.readFileSync('plan.md', 'utf8');
            const { data: comments } = await github.rest.issues.listComments({
              owner: context.repo.owner,
              repo: context.repo.repo,
              issue_number: context.issue.number,
            });
            const existing = comments.find(c => c.body.includes('### Crossplane Validation'));
            const params = {
              owner: context.repo.owner,
              repo: context.repo.repo,
              body: body || '### Crossplane Validation\n\nNo changes detected.',
            };
            if (existing) {
              await github.rest.issues.updateComment({ ...params, comment_id: existing.id });
            } else {
              await github.rest.issues.createComment({ ...params, issue_number: context.issue.number });
            }
```

---

## Configuration

Create `.crossplane-validate.yml` in your repo root (optional):

```yaml
# Directories containing Crossplane manifests
manifests:
  - crossplane/
  - infra/crossplane/

# Cloud provider credentials for --cloud mode
# Providers are auto-detected from manifests — only add entries
# here if you need to pass specific config (region, project, subscription).
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

# Optional settings
settings:
  timeout: 10m
  diff-format: terraform
  ignore-fields:
    - metadata.resourceVersion
    - metadata.uid
    - status.conditions
```

Without a config file, `crossplane-validate` uses sensible defaults and scans the current directory. Providers are auto-detected from the manifests regardless of whether a config file exists.

---

## Prerequisites

| Requirement | For |
|---|---|
| **Git** | Always required (for branch comparison) |
| **Go 1.26+** | Only if building from source |
| **Terraform or OpenTofu** | Only for `--cloud` mode |
| **Cloud credentials** | Only for `--cloud` mode (read-only recommended) |

For basic validation, all you need is the binary and a git repo.

---

## Examples

### New resource detection

When a manifest is added in a feature branch but doesn't exist on main:

```
$ crossplane-validate plan --manifests=./crossplane/

═══ Structural Changes ═══

  + Account/stcpvalidatetest01
      + spec.forProvider.accountReplicationType: LRS
      + spec.forProvider.accountTier: Standard
      + spec.forProvider.location: australiaeast
      + spec.forProvider.resourceGroupName: rg-sdbx-np-dev
      + spec.forProvider.tags.Environment: devtest
      + spec.forProvider.tags.ManagedBy: crossplane

Plan: 1 to add, 0 to change, 0 to destroy
```

### Modification detection

When an existing manifest is changed:

```
$ crossplane-validate plan --manifests=./crossplane/

═══ Structural Changes ═══

  ~ Account/stcpvalidatetest01
      ~ spec.forProvider.accountReplicationType: LRS → GRS
      + spec.forProvider.allowNestedItemsToBePublic: false
      + spec.forProvider.tags.CostCenter: platform-engineering

Plan: 0 to add, 1 to change, 0 to destroy
```

### Cloud-aware plan (real Terraform diff)

```
$ crossplane-validate plan --cloud --manifests=./crossplane/

═══ Structural Changes ═══

  ~ Account/stcpvalidatecloud01
      ~ spec.forProvider.accountReplicationType: LRS → GRS
      + spec.forProvider.allowNestedItemsToBePublic: false
      + spec.forProvider.tags.CostCenter: platform-engineering
      ~ spec.forProvider.tags.Environment: devtest → production

═══ Cloud Impact ═══

  ~ azurerm_storage_account.stcpvalidatecloud01
      ~ account_replication_type: LRS → GRS
      ~ allow_nested_items_to_be_public: true → false
      ~ tags: {Environment:devtest ...} → {environment:production, cost_center:platform-engineering ...}

Plan: 0 to add, 1 to change, 0 to destroy
Cloud: 0 to add, 1 to change, 0 to destroy
```

### Deletion detection

```
$ crossplane-validate plan --manifests=./crossplane/

═══ Structural Changes ═══

  - VPC/deprecated-vpc
  - Subnet/deprecated-subnet

Plan: 0 to add, 0 to change, 2 to destroy
```

### Multi-provider in one plan

```
$ crossplane-validate plan --manifests=./crossplane/

═══ Structural Changes ═══

  + Bucket/logs-bucket                    (AWS)
      + spec.forProvider.region: us-east-1

  ~ DatabaseInstance/app-db               (GCP)
      ~ spec.forProvider.tier: db-f1-micro → db-n1-standard-2

  - ResourceGroup/old-rg                  (Azure)

Plan: 1 to add, 1 to change, 1 to destroy
```

---

## Project Structure

```
crossplane-validation/
├── cmd/crossplane-validate/     # CLI entry point
├── pkg/
│   ├── config/                  # Configuration loading
│   ├── manifest/                # YAML scanner and resource classifier
│   ├── renderer/                # Composition rendering engine
│   ├── diff/                    # Structural diff computation
│   ├── hcl/                     # Crossplane → HCL converter + schema lookup
│   ├── plan/                    # Plan output rendering (terminal, markdown, JSON)
│   └── tofu/                    # Terraform/OpenTofu integration
├── internal/git/                # Git operations
├── testdata/                    # Test manifests (AWS, GCP, Azure)
├── .github/
│   ├── workflows/               # CI, Release, Validate workflows
│   ├── ISSUE_TEMPLATE/          # Bug report and feature request forms
│   ├── CODEOWNERS               # Required reviewers
│   └── pull_request_template.md
├── action.yml                   # GitHub Action definition
├── ROADMAP.md                   # Planned features
├── LICENSE                      # Apache 2.0
├── CONTRIBUTING.md
├── CODE_OF_CONDUCT.md
└── SECURITY.md
```

---

## Roadmap

See [ROADMAP.md](ROADMAP.md) for planned features including:

- Additional provider support (Cloudflare, DigitalOcean, MongoDB Atlas, Vault, etc.)
- Policy engine (OPA/Rego)
- Cost estimation
- Drift detection
- ArgoCD pre-sync integration
- Docker image and Krew plugin

---

## Contributing

We welcome contributions! See [CONTRIBUTING.md](CONTRIBUTING.md) for details.

**Good first contributions:**
- Add resource mappings for new Crossplane providers
- Improve composition rendering for edge cases
- Add test coverage for additional resource types
- Documentation and examples

---

## Security

- Use **read-only** cloud credentials for `--cloud` mode
- Never commit credentials to your repository
- Report vulnerabilities to unidevidp@gmail.com (see [SECURITY.md](SECURITY.md))

---

## License

Apache License 2.0 — see [LICENSE](LICENSE) for details.

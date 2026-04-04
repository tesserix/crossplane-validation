<p align="center">
  <img src="https://img.shields.io/github/v/release/tesserix/crossplane-validation?style=flat-square&color=blue" alt="Release">
  <img src="https://img.shields.io/github/actions/workflow/status/tesserix/crossplane-validation/ci.yml?branch=main&style=flat-square&label=CI" alt="CI">
  <img src="https://img.shields.io/github/license/tesserix/crossplane-validation?style=flat-square" alt="License">
  <img src="https://img.shields.io/github/go-mod/go-version/tesserix/crossplane-validation?style=flat-square" alt="Go Version">
  <img src="https://img.shields.io/badge/crossplane-compatible-blueviolet?style=flat-square" alt="Crossplane Compatible">
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
| **Cloud-aware plan** | Optional: converts to HCL and runs OpenTofu for real cloud-state diff |
| **Composition rendering** | Renders Patch-and-Transform and Function Pipeline compositions offline |
| **Multi-provider** | AWS, GCP, Azure — with automatic provider detection |
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

### Cloud-aware plan (Mode 3)

For deeper validation, enable cloud mode. This converts your Crossplane resources to HCL and runs OpenTofu with read-only credentials to show the actual cloud impact:

```bash
crossplane-validate plan --manifests=./crossplane/ --cloud
```

This requires a `.crossplane-validate.yml` config with credentials (see [Configuration](#configuration)).

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
    │  main vs feature  │          │  MR → Terraform    │
    │  field-level diff │          │  OpenTofu plan     │
    └─────────┬─────────┘          └─────────┬─────────┘
              │                               │
              └───────────┬───────────────────┘
                          │
                ┌─────────┴─────────┐
                │   Plan Renderer   │
                │  terminal / md /  │
                │  json output      │
                └───────────────────┘
```

**Mode 1 (default):** Git-based diff. Renders manifests from both branches, diffs the output. Fast, offline, no credentials needed.

**Mode 3 (--cloud):** Cloud-aware plan. Converts Crossplane managed resources to equivalent Terraform HCL (the mapping is 1:1 because Crossplane's Upjet providers are generated from Terraform providers), then runs OpenTofu plan with read-only credentials.

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

      - uses: tesserix/crossplane-validation@v0.1.0
        with:
          manifests: ./crossplane/
```

### Action inputs

| Input | Default | Description |
|---|---|---|
| `version` | `latest` | Version to install (e.g., `v0.1.0`) |
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
providers:
  aws:
    credentials: env          # reads from AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY
    region: us-east-1
  gcp:
    credentials: env          # reads from GOOGLE_APPLICATION_CREDENTIALS
    project: my-project
  azure:
    credentials: env          # reads from ARM_CLIENT_ID / ARM_CLIENT_SECRET / ARM_TENANT_ID

# Optional settings
settings:
  timeout: 10m
  diff-format: terraform      # "terraform" or "yaml"
  ignore-fields:
    - metadata.resourceVersion
    - metadata.uid
    - status.conditions
```

Without a config file, `crossplane-validate` uses sensible defaults and scans the current directory.

---

## Supported Resources

### AWS (provider-aws)

| Service | Resources |
|---|---|
| **S3** | Bucket, BucketPolicy, BucketACL, BucketVersioning |
| **IAM** | Role, RolePolicy, RolePolicyAttachment, Policy, User, Group |
| **EC2** | VPC, Subnet, SecurityGroup, Instance, InternetGateway, RouteTable, NATGateway, EIP |
| **RDS** | Instance, Cluster, SubnetGroup |
| **EKS** | Cluster, NodeGroup |

### GCP (provider-gcp)

| Service | Resources |
|---|---|
| **Compute** | Instance, Network, Subnetwork, Firewall |
| **GKE** | Cluster, NodePool |
| **Cloud SQL** | DatabaseInstance, Database, User |
| **Storage** | Bucket |
| **IAM** | ServiceAccount |

### Azure (provider-azure)

| Service | Resources |
|---|---|
| **Resources** | ResourceGroup |
| **Network** | VirtualNetwork, Subnet |
| **Storage** | Account, Container |
| **Compute** | LinuxVirtualMachine |

Additional resources are detected automatically via Crossplane API group conventions. Unmapped resources fall back to best-effort naming.

---

## Prerequisites

| Requirement | For |
|---|---|
| **Git** | Always required (for branch comparison) |
| **Go 1.26+** | Only if building from source |
| **OpenTofu or Terraform** | Only for `--cloud` mode |
| **Cloud credentials** | Only for `--cloud` mode (read-only recommended) |

For basic validation (Mode 1), all you need is the binary and a git repo.

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

### Deletion detection

When a manifest is removed from the feature branch:

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

  + Bucket/logs-bucket
      + spec.forProvider.region: us-east-1

  ~ DatabaseInstance/app-db
      ~ spec.forProvider.tier: db-f1-micro → db-n1-standard-2

  - ResourceGroup/old-rg

Plan: 1 to add, 1 to change, 1 to destroy
```

---

## Project Structure

```
crossplane-validation/
├── cmd/crossplane-validate/     # CLI entry point
├── pkg/
│   ├── config/                  # Configuration loading
│   ├── manifest/                # YAML scanner and classifier
│   ├── renderer/                # Composition rendering engine
│   ├── diff/                    # Structural diff computation
│   ├── hcl/                     # Crossplane → Terraform HCL converter
│   ├── plan/                    # Plan output rendering
│   └── tofu/                    # OpenTofu integration
├── internal/git/                # Git operations
├── testdata/                    # Test manifests (AWS, GCP, Azure)
├── .github/
│   ├── workflows/               # CI, Release, Validate workflows
│   ├── ISSUE_TEMPLATE/          # Bug report and feature request forms
│   ├── CODEOWNERS               # Required reviewers
│   └── pull_request_template.md
├── action.yml                   # GitHub Action definition
├── LICENSE                      # Apache 2.0
├── CONTRIBUTING.md
├── CODE_OF_CONDUCT.md
└── SECURITY.md
```

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
- Report vulnerabilities to samyak.rout@gmail.com (see [SECURITY.md](SECURITY.md))

---

## License

Apache License 2.0 — see [LICENSE](LICENSE) for details.

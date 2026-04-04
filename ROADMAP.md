# Roadmap

A living document tracking planned features and improvements.

---

## Completed

- [x] Git-based structural diff (`plan`)
- [x] Composition rendering (Patch-and-Transform + Function Pipeline)
- [x] Multi-provider support (AWS, GCP, Azure in one plan)
- [x] Cloud-aware plan via Terraform/OpenTofu (`--cloud`)
- [x] Dynamic resource type resolution from provider schema
- [x] Auto-import existing cloud resources for update diffs
- [x] Auto-detect providers from manifests
- [x] `scan` command for resource discovery
- [x] Homebrew distribution
- [x] GitHub Action
- [x] Multi-platform binaries (macOS, Linux, Windows)
- [x] PR comment output (markdown format)

---

## Planned

### Provider Coverage

- [ ] Cloudflare (`cloudflare.upbound.io` → `cloudflare/cloudflare`)
- [ ] DigitalOcean (`digitalocean.upbound.io` → `digitalocean/digitalocean`)
- [ ] MongoDB Atlas (`mongodbatlas.upbound.io` → `mongodb/mongodbatlas`)
- [ ] Kubernetes (`kubernetes.crossplane.io` → `hashicorp/kubernetes`)
- [ ] Helm (`helm.crossplane.io` → `hashicorp/helm`)
- [ ] GitHub (`github.upbound.io` → `integrations/github`)
- [ ] Vault (`vault.upbound.io` → `hashicorp/vault`)
- [ ] Confluent (`confluent.upbound.io` → `confluentinc/confluent`)
- [ ] Grafana (`grafana.upbound.io` → `grafana/grafana`)
- [ ] PagerDuty (`pagerduty.upbound.io` → `pagerduty/pagerduty`)

### Core Features

- [ ] **Policy engine** — OPA/Rego policy checks on manifests (naming, tags, regions, cost controls)
- [ ] **Cost estimation** — estimate cloud costs for planned changes (like Infracost)
- [ ] **Drift detection** — compare git state vs live cluster state vs cloud state
- [ ] **Composition function support** — full offline rendering for `function-go-templating` and custom functions
- [ ] **Dependency graph** — visualize resource relationships and creation order
- [ ] **Destructive change warnings** — flag operations that cause downtime or data loss (e.g., DB engine change, storage account deletion)

### CLI Improvements

- [ ] **Interactive mode** — step through changes one by one with approve/skip
- [ ] **Config auto-detection** — detect provider credentials from environment without explicit config
- [ ] **Parallel provider schema loading** — speed up `--cloud` mode for multi-provider setups
- [ ] **JSON output** — structured output for programmatic consumption
- [ ] **`plan --watch`** — continuous plan on file changes during development
- [ ] **`validate` command** — schema validation without diff (check YAML is valid against CRDs)

### CI/CD Integration

- [ ] **GitLab CI template** — reusable pipeline for GitLab
- [ ] **Azure DevOps task** — native ADO pipeline task
- [ ] **Bitbucket Pipes** — Bitbucket pipeline integration
- [ ] **ArgoCD integration** — pre-sync hook that runs validation before ArgoCD applies
- [ ] **Slack/Teams notifications** — post plan summaries to chat

### Distribution

- [ ] **Docker image** — `ghcr.io/tesserix/crossplane-validate`
- [ ] **Nix package** — for NixOS users
- [ ] **Chocolatey package** — for Windows users
- [ ] **Krew plugin** — install as `kubectl crossplane-validate`

---

## Contributing

Have an idea not listed here? Open a [feature request](https://github.com/tesserix/crossplane-validation/issues/new?template=feature_request.yml).

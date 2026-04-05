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
- [x] Auto-detect credentials from environment (env vars, CLI profiles, OIDC, managed identity)
- [x] `scan` command for resource discovery
- [x] `validate` command — schema validation against XRD CRDs
- [x] JSON output (`--output=json`) for programmatic consumption
- [x] Destructive change warnings — flags operations that cause downtime or data loss
- [x] Homebrew distribution
- [x] GitHub Action
- [x] Multi-platform binaries (macOS, Linux, Windows)
- [x] PR comment output (markdown format)
- [x] **In-cluster operator** — watches Crossplane resources, caches live state via dynamic informers
- [x] **Live plan mode** (`--live`) — compare proposed manifests against actual cluster state
- [x] **Drift detection** — compare git manifests vs live cluster state (`drift` command)
- [x] **Cluster status** — view resource health and readiness (`status` command)
- [x] **gRPC API** — operator exposes gRPC service for CLI and CI connectivity
- [x] **Slack/Teams notifications** — post plan summaries to chat channels
- [x] **Docker image** — `ghcr.io/tesserix/crossplane-validate-operator` (public)
- [x] **Helm chart** — deploy operator with `helm install`
- [x] **Kustomize manifests** — alternative operator deployment
- [x] **Live CI workflow** — GitHub Actions workflow for live plan PR comments

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
- [ ] **Composition function support** — full offline rendering for `function-go-templating` and custom functions
- [ ] **Dependency graph** — visualize resource relationships and creation order
- [ ] **Validating admission webhook** — block dangerous changes before Crossplane reconciles them

### CLI Improvements

- [ ] **Interactive mode** — step through changes one by one with approve/skip
- [ ] **Parallel provider schema loading** — speed up `--cloud` mode for multi-provider setups
- [ ] **`plan --watch`** — continuous plan on file changes during development

### Operator Enhancements

- [ ] **ValidationPolicy CRD** — define rules (max instance size, required tags, blocked regions)
- [ ] **ValidationResult CRD** — persist plan outputs for audit trail
- [ ] **ArgoCD pre-sync hook** — run validation before ArgoCD syncs resources
- [ ] **mTLS** — certificate-based auth between CLI and operator
- [ ] **Multi-cluster** — CLI queries multiple operators, aggregated view

### CI/CD Integration

- [ ] **GitLab CI template** — reusable pipeline for GitLab
- [ ] **Azure DevOps task** — native ADO pipeline task
- [ ] **Bitbucket Pipes** — Bitbucket pipeline integration

### Distribution

- [ ] **Nix package** — for NixOS users
- [ ] **Chocolatey package** — for Windows users
- [ ] **Krew plugin** — install as `kubectl crossplane-validate`

---

## Contributing

Have an idea not listed here? Open a [feature request](https://github.com/tesserix/crossplane-validation/issues/new?template=feature_request.yml).

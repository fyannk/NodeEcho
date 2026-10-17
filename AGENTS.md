# AGENTS.md — NodeEcho

You are working on **NodeEcho**, a Kubernetes controller that echoes Node
facts (cordon, selected taints, labels and annotations) onto the pods running
there, so namespace-scoped operators need no access to Nodes. CloudNativePG is
the first consumer in view; nothing in NodeEcho is specific to it.

The code is the source of truth. The contract
(`web/docs/reference/contract.md`) says what consumers may rely on; the rest
of the Docusaurus site in `web/docs/` explains implemented behavior. A change
of behavior updates the contract and the site in the same change.

## Design principles (not negotiable)

- **Plain echo.** Copy Node facts as they are; never derive a decision such
  as "draining". Each consumer keeps its own policy.
- **A key only when it says something.** `unschedulable` is `"true"` or
  absent; the taints annotation is absent when no listed taint is there. No
  `false`, no `[]`, no per-pod bookkeeping (versions, heartbeats, source UIDs).
  A healthy pod is never written.
- **Never another writer's key.** Server-side apply, one field manager per
  rule (`nodeecho/<rule>`; `nodeecho-eviction/<rule>` for eviction marks); a
  key owned by someone else is skipped with a Warning Event.
- **Fail safe.** Until a first valid configuration is loaded, write and remove
  nothing. An invalid configuration keeps the last valid one; a refused
  running rule keeps its last version. The eviction webhook uses
  `failurePolicy: Ignore`.
- **Only NodeEcho's decisions belong here.** Consumer roadmaps (how or when
  CNPG integrates) are not NodeEcho decisions.

## Layout

| Path | Contents |
| --- | --- |
| `cmd/nodeecho` | the manager: flags, logging, metrics, webhook server, certificate rotation |
| `internal/config` | parsing and validation of the rules (100% covered, fuzzed) |
| `internal/projection` | pure: a rule and a Node give the pod keys (fuzzed) |
| `internal/controller` | rules loader, pod projector, eviction webhook, cache transforms, metrics |
| `internal/logstyle` | a test enforcing the logging conventions on every log call |
| `config/` | kustomize: `default`, `rbac`, `manager`, `policy`, `prometheus`, `grafana`, `components/` |
| `deploy/helm/nodeecho` | Helm chart |
| `deploy/olm` | OLM bundle (CSV) and file-based catalog |
| `hack/` | `e2e.sh`, `scale.sh`, `test-alerts.sh`, `validate-packaging.sh`, `check-go-version.sh` |
| `web/` | documentation site (Docusaurus), deployed to GitHub Pages |

## Building and testing

```bash
make test                 # unit, fuzz seeds, envtest (downloads API server binaries)
make lint                 # golangci-lint (incl. loggercheck, forbidigo), Go toolchain check
make vuln                 # govulncheck
make test-alerts          # promtool check and unit tests of the alerts (docker)
make validate-packaging   # manifests, Helm chart, OLM bundle and catalog agree
make e2e                  # throwaway k3d clusters, six variants (see hack/e2e.sh)
make scale                # memory, API requests and latency with many pods
cd web && npm ci && npm run typecheck && npm run build
```

- Judge a command by its exit code, never by filtered output.
- envtest runs against Kubernetes 1.36.2 by default; CI also runs 1.34.1
  (`ENVTEST_K8S_VERSION`). Tests must pass on both.
- New behavior needs a test that fails without it; for a bug, show the test
  failing on the old code.
- `make e2e` variants: `k8s-1.34`, `k8s-1.36`, `gate-off`
  (`PodTopologyLabelsAdmission` off), `cert-manager`, `helm`, `olm`. Pick
  some with `E2E_VARIANTS`; `E2E_KEEP=1` keeps the clusters. Behind an HTTP
  proxy, `E2E_NETWORK` names a Docker network that reaches it.

## Cluster safety

- **Never use the default kubeconfig or its current context.** Every cluster
  command names an explicit kubeconfig of a throwaway cluster. The scripts in
  `hack/` create their own k3d clusters (`nodeecho-e2e-*`, `nodeecho-scale`)
  with a kubeconfig in a temporary directory, check the context, and delete
  the clusters at the end.
- Touch no cluster you did not create.

## Conventions

- Go 1.27 (toolchain pinned in `go.mod`, matched by the Dockerfile's builder;
  `hack/check-go-version.sh`), controller-runtime 0.25, k8s.io libraries 0.37
  (keep all `k8s.io/*` modules on the same patch version).
- **Logging:** JSON through controller-runtime's logr logger. Messages are
  constant, start with a capital letter, no trailing period, no format verbs;
  everything variable goes in lowerCamelCase key-value pairs. Enforced by
  `internal/logstyle`, `loggercheck` and `forbidigo` (no `fmt.Print*` or
  standard `log` calls outside tests).
- **Metrics:** `nodeecho_*`; every metric used by `config/prometheus/alerts.yaml`
  or the dashboard must be exposed (the e2e test checks it); every alert has a
  promtool test in `config/prometheus/alerts_test.yaml`.
- **Packaging:** the kustomize manifests, the Helm chart and the OLM bundle
  are written by hand; `make validate-packaging` (CI) checks that versions,
  permissions, ports, environment and copied files agree. A change to
  `config/` usually needs the same change in `deploy/`.
- **Admission policies:** both name NodeEcho's ServiceAccount; the Helm chart
  computes the writer-scope allowed list from the rules.
- **Releases:** a `v*` tag publishes the multi-arch image, the OLM images and
  the chart; the tag must equal the chart's version.
- **Dependencies:** Dependabot covers Go, GitHub Actions and Docker, not
  `web/`. In `web/package.json`, only libraries shipped to the browser are
  `dependencies`.

## Commits and GitHub

- Conventional Commits, compact: a subject line and a short what/why body.
  No issue or PR numbers in commit messages.
- Commits are signed off (`git commit -s`) by the human author, and carry no
  tool attribution trailers (no `Co-Authored-By` or `Generated with` lines).
- Never name the maintainer's employer anywhere in the repository.
- Propose commit messages and wait for the maintainer's approval before
  committing; never push, open or comment on issues and PRs without explicit
  approval of the exact text.

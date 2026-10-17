---
sidebar_position: 7
title: Development
---

# Development

Go and controller-runtime. `make help` lists the targets.

```bash
make build    # bin/nodeecho
make test     # unit, fuzz-seed and envtest tests
make lint     # golangci-lint and the toolchain check
make vuln     # govulncheck
make test-alerts  # promtool check and unit tests of the alerts (docker)
make validate-packaging  # manifests, Helm chart, OLM bundle and catalog agree
```

## Layout

| Path | Contents |
| --- | --- |
| `internal/config` | parsing and validation of the rules |
| `internal/projection` | pure: a rule and a Node give the pod keys |
| `internal/controller` | the rules loader, the pod projector, the cache transforms |
| `cmd/nodeecho` | the manager |
| `config/` | kustomize manifests: `default`, `rbac`, `manager`, `policy` |
| `hack/` | the end-to-end and scale scripts |
| `web/` | this site |

## Tests

- **Unit and fuzz tests** for the parser and the projection. Run a fuzzer
  longer with, for instance,
  `go test ./internal/config -run '^$' -fuzz FuzzParse -fuzztime 60s`.
- **envtest**: `make test` downloads a real API server and etcd
  (`ENVTEST_K8S_VERSION`, default 1.36.2) and runs the controller against
  them.
- **End to end**: `make e2e` runs NodeEcho on throwaway k3d clusters, one per
  variant: Kubernetes 1.34, 1.36, 1.36 with `PodTopologyLabelsAdmission`
  off, cert-manager certificates, the Helm chart, and OLM from the bundle and
  catalog served by a local registry. Needs Docker, k3d, kubectl and jq; helm
  and operator-sdk for their variants. `E2E_VARIANTS` picks some.
- **Scale**: `make scale` measures memory, API requests and latency with
  thousands of pods on fake Nodes.

The two scripts use their own kubeconfig per cluster and never read or
write the default one; they delete their clusters at the end unless
`E2E_KEEP=1` or `SCALE_KEEP=1`. Behind an HTTP proxy, set `E2E_NETWORK` to a
Docker network that reaches it; the proxy variables are passed to the image
build and to the k3d nodes.

## Logging

Logs go through controller-runtime's logr logger, as JSON, following the
[Kubernetes structured logging guidelines](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-instrumentation/logging.md):

- the message is a constant string saying what happened, starting with a
  capital letter, without a trailing period or format verbs:
  `logger.Info("Rule refused", "rule", name)`, not
  `logger.Info("rule " + name + " refused.")`;
- everything variable goes in key-value pairs, with lowerCamelCase keys;
- errors go through `logger.Error(err, "…")`;
- level 0 for what an operator acts on, `V(1)` for per-pod detail.

Three checks enforce it: `internal/logstyle` (a test that parses every log
call: message literal, capital, punctuation, keys), and two linters in
`make lint`: `loggercheck` (key-value pairs) and `forbidigo` (no
`fmt.Print*` or standard `log` calls outside tests). The end-to-end test
checks that every line NodeEcho prints is JSON.

## Monitoring

`make test-alerts` checks `config/prometheus/alerts.yaml` with promtool and
runs its unit tests, `config/prometheus/alerts_test.yaml`, in pinned
containers. Add a test with every new alert. The end-to-end test checks that
every metric the alerts and the dashboard use is exposed.

## Packaging

The kustomize manifests (`config/`), the Helm chart (`deploy/helm`) and the
OLM bundle and catalog (`deploy/olm`) are written by hand.
`make validate-packaging` checks that they agree: one version everywhere,
the same permissions, ports and environment, the chart's and the bundle's
copies of the `config/` files; it also lints and renders the chart in its
variants and validates the bundle and the catalog when `operator-sdk` and
`opm` are installed. CI runs it.

A `v*` tag publishes the multi-arch image, the bundle and catalog images, and
the chart (`images.yml`, `release.yml`); the tag must match the chart's
version. Every image and the chart are signed keyless with cosign by the
workflow and get a GitHub build provenance attestation; the NodeEcho image
also carries an SBOM and a provenance attestation from BuildKit.
[Installation](operator/installation.md) says how to verify them.

## Dependencies

Dependabot follows the Go module, the GitHub Actions and the image bases,
not the documentation site: its build tooling is most of the noise and none
of it ships with NodeEcho. In `web/package.json`, the libraries that reach
the reader's browser (React, MDX, Prism) are `dependencies`; everything else
is a `devDependency`, which GitHub's default auto-triage dismisses as
development-scoped when Dependabot alerts are on.

## Contributing

[CONTRIBUTING.md](https://github.com/fyannk/NodeEcho/blob/main/CONTRIBUTING.md)
in the repository says how a change gets in: one change per pull request,
signed-off Conventional Commits, a test that fails without the change, the
contract and this site updated in the same change, and AI assistance
disclosed wherever it appears.

## This site

```bash
cd web
npm ci
npm start       # live preview
npm run build   # static site in web/build
```

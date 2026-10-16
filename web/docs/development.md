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
- **End to end**: `make e2e` runs NodeEcho on throwaway k3d clusters for
  Kubernetes 1.34, 1.36, and 1.36 with `PodTopologyLabelsAdmission` off.
  Needs Docker, k3d, kubectl and jq.
- **Scale**: `make scale` measures memory, API requests and latency with
  thousands of pods on fake Nodes.

The two scripts use their own kubeconfig per cluster and never read or
write the default one; they delete their clusters at the end unless
`E2E_KEEP=1` or `SCALE_KEEP=1`. Behind an HTTP proxy, set `E2E_NETWORK` to a
Docker network that reaches it; the proxy variables are passed to the image
build and to the k3d nodes.

## This site

```bash
cd web
npm ci
npm start       # live preview
npm run build   # static site in web/build
```

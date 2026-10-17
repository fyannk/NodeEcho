# NodeEcho

**Kubernetes node context, reflected onto pods.**

NodeEcho is a Kubernetes controller that echoes selected node metadata and
scheduling state onto the pods running on those nodes. It lets
namespace-scoped operators react to infrastructure changes without their own
cluster-wide access to Nodes.

The initial use case is CloudNativePG: preserve cordon/drain-aware switchovers
and topology awareness when direct Node access is disabled in a modified
operator.

- Reflects node cordon state and selected taints onto bound pods.
- Copies explicitly allowed node labels and annotations, including custom
  topology.
- Limits propagation to selected namespaces, workloads and nodes.
- Writes nothing when there is nothing to echo, and never overwrites another
  writer's keys.
- Optionally records eviction requests on the pods, and answers them.

Install it with the Helm chart (`deploy/helm/nodeecho`), kustomize
(`config/`) or OLM (`deploy/olm`); see
[Installation](web/docs/operator/installation.md).

**Status: early implementation, not production-ready.** Contract v1 is
implemented, tested against a real API server (envtest) and end to end on k3d
clusters (Kubernetes 1.34, 1.36, 1.36 with `PodTopologyLabelsAdmission`
off, and each install path). No version is released yet. Upstream CloudNativePG does not consume
NodeEcho metadata; an integration is required. Compatibility with individual
forks remains to be validated.

Kubernetes 1.35 already provides zone and region labels on newly assigned pods
through `PodTopologyLabelsAdmission`. NodeEcho focuses on dynamic node state,
custom metadata, and compatibility needs beyond that native feature.

## Documentation

The documentation site is built from [`web/`](web/docs/):

- [Introduction](web/docs/overview/index.md) and [status](web/docs/overview/status.md)
- [Getting started](web/docs/tutorials/getting-started.md)
- [Configuration](web/docs/operator/configuration.md),
  [eviction webhook](web/docs/operator/eviction-webhook.md),
  [admission policies](web/docs/operator/admission-policies.md) and
  [observability](web/docs/operator/observability.md): metrics, alerts,
  dashboard, logs, pprof
- [Contract v1](web/docs/reference/contract.md): the keys a consumer sees on
  its pods, and what it may rely on
- [Design notes](web/docs/design/design.md) and research on
  [CloudNativePG](web/docs/research/cloudnative-pg.md), the
  [ecosystem](web/docs/research/ecosystem.md) and
  [about twenty operators](web/docs/research/operators.md), checked on
  **2026-10-16**

## Development

Go and controller-runtime; `make help` lists the targets.

```sh
make test     # unit, fuzz-seed and envtest tests
make lint     # golangci-lint, logging rules and the toolchain check
make test-alerts  # promtool check and unit tests of the alerts (docker)
make validate-packaging  # manifests, Helm chart, OLM bundle and catalog agree
make build    # bin/nodeecho
make e2e      # end-to-end on throwaway k3d clusters: 1.34, 1.36, 1.36 with
              # PodTopologyLabelsAdmission off, cert-manager, Helm, OLM
make scale    # memory, API requests and latency with thousands of pods on
              # fake Nodes, on a throwaway k3d cluster
make deploy   # kustomize config/default into nodeecho-system
```

`make e2e` and `make scale` use their own kubeconfig per cluster and never
touch the default one. Set `E2E_NETWORK` to attach the k3d nodes to an
existing Docker network, for instance one that reaches an HTTP proxy. See
[Development](web/docs/development.md) for more.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md): one change per pull request,
signed-off Conventional Commits, and AI assistance disclosed wherever it
appears. [AGENTS.md](AGENTS.md) holds the rules for anyone, human or agent,
working in the repository.

## Security

Report vulnerabilities privately, see [SECURITY.md](SECURITY.md).

## License

Apache License 2.0, see [LICENSE](LICENSE).

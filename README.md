# NodeEcho

**Kubernetes node context, reflected onto pods.**

NodeEcho is a proposed Kubernetes controller that projects selected node metadata
and scheduling state onto the pods running on those nodes. Its goal is to let
namespace-scoped operators react to infrastructure changes without requiring
their own cluster-wide access to Nodes.

The initial use case is CloudNativePG: preserve cordon/drain-aware switchovers
and topology awareness when direct Node access is disabled in a modified
operator.

Planned capabilities include:

- Continuously reflect node cordon state and selected taints onto bound pods.
- Copy explicitly allowed node labels and annotations, including custom topology.
- Limit propagation to selected namespaces and workloads.
- Write nothing when there is nothing to echo, and never overwrite another writer's keys.

**Status: early implementation.** Contract v1 is drafted and implemented,
tested against a real API server (envtest); not yet run on a cluster. Upstream
CloudNativePG does not automatically consume NodeEcho metadata; an integration
is required. Compatibility with individual forks remains to be validated.

Kubernetes 1.35 already provides zone and region labels on newly assigned pods
through `PodTopologyLabelsAdmission`. NodeEcho focuses on dynamic node state,
custom metadata, and compatibility needs beyond that native feature.

See [the documentation](docs/README.md) for the research, sources, and proposed
design. Research was checked on **2026-10-16**; development-branch features are
identified separately from released behavior.

## Development

Go and controller-runtime; `make help` lists the targets.

```sh
make test     # unit tests
make lint     # golangci-lint and the toolchain check
make build    # bin/nodeecho
make deploy   # kustomize config/default into nodeecho-system
```

## License

Apache License 2.0, see [LICENSE](LICENSE).

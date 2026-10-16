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
- Expose enough source information to distinguish missing or outdated context.

**Status: research and design. No controller is implemented yet.** Upstream
CloudNativePG does not automatically consume NodeEcho metadata; an integration
is required. Compatibility with individual forks remains to be validated.

Kubernetes 1.35 already provides zone and region labels on newly assigned pods
through `PodTopologyLabelsAdmission`. NodeEcho focuses on dynamic node state,
custom metadata, and compatibility needs beyond that native feature.

See [the documentation](docs/README.md) for the research, sources, and proposed
design. Research was checked on **2026-10-16**; development-branch features are
identified separately from released behavior.

# Ecosystem research

Research checked: **2026-10-16**.

## Kubernetes-native facilities

### Zone and region on pods

`PodTopologyLabelsAdmission` is beta and enabled by default from Kubernetes
1.35. The built-in admission controller copies
`topology.kubernetes.io/zone` and `topology.kubernetes.io/region` from the assigned
node onto the pod during assignment, when those labels exist.

This is not a general continuous synchronization of arbitrary node metadata.
Enabling the feature does not retroactively populate already-running pods.

Sources:

- [Admission controller reference](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/#podtopologylabels)
- [Feature gates](https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/)
- [KEP-4742](https://github.com/kubernetes/enhancements/issues/4742)
- [KEP rendered by Kubernetes Contributors](https://www.kubernetes.dev/resources/keps/4742/)

The list is fixed in the v1.35 code: the plugin copies only zone and region,
**not** `kubernetes.io/hostname`, although the KEP text also names it
([admission.go](https://github.com/kubernetes/kubernetes/blob/v1.35.0/plugin/pkg/admission/podtopologylabels/admission.go#L43-L45),
[kube_features.go](https://github.com/kubernetes/kubernetes/blob/v1.35.0/pkg/features/kube_features.go#L1650-L1653)).
Hostname and custom keys (racks, rooms, readiness labels) remain to be copied
by another component.

The binding subresource copies the Binding's annotations onto the pod and, with
`PodTopologyLabelsAdmission` on, all of its labels
([storage.go](https://github.com/kubernetes/kubernetes/blob/v1.35.0/pkg/registry/core/pod/storage/storage.go#L253-L260)).
A mutating webhook on `pods/binding` can therefore add node metadata atomically
at binding: Envoy Gateway copies the node zone into Binding annotations that way
([topology_injector.go](https://github.com/envoyproxy/gateway/blob/f74a274b10/internal/provider/kubernetes/topology_injector.go#L77-L103)),
and [issue #7443](https://github.com/envoyproxy/gateway/issues/7443) plans to
replace it with KEP-4742. Annotations are proven by Envoy; labels are inferred
from the 1.35 storage code only.

The KEP explicitly discusses a helper controller and its inability to guarantee
metadata is present before a pod starts. This distinction matters for consumers
that read topology through environment variables.

### Cordon, drain, and eviction are different signals

| Signal | Meaning | What cannot be inferred |
| --- | --- | --- |
| `Node.spec.unschedulable=true` | Normal scheduling to the node is disabled | A drain was started or completed |
| A configured taint | The producer's documented scheduling/maintenance signal | A universal Kubernetes drain phase |
| A `pods/eviction` request | An actor requested eviction of one pod | That the request will succeed or came from a whole-node drain |
| `DisruptionTarget` / `EvictionByEvictionAPI` | The pod was marked for API-initiated eviction | The entire node is draining, or the eviction will necessarily complete |
| A pod `deletionTimestamp` | Deletion has been requested | The process stopped, or why deletion was requested |

`kubectl drain` first makes the node unschedulable, then requests evictions or
deletions. There is no universal `Node.status.draining` field for a reflector
to copy. DaemonSet and mirror pods have special handling; an empty-node
heuristic does not define a reliable maintenance lifecycle.

Sources:

- [`kubectl drain`](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_drain/)
- [Safely drain a node](https://kubernetes.io/docs/tasks/administer-cluster/safely-drain-node/)
- [Pod disruption conditions](https://kubernetes.io/docs/concepts/workloads/pods/disruptions/)

## Strimzi: separating infrastructure signals from application decisions

### Drain Cleaner

[Strimzi Drain Cleaner](https://github.com/strimzi/drain-cleaner) is the strongest
precedent for a separate maintenance-signal component:

1. An admission webhook receives a pod eviction request.
2. It reads the target pod and identifies supported Strimzi workloads.
3. It patches `strimzi.io/manual-rolling-update` onto the pod.
4. In its default documented mode it denies the eviction request.
5. Strimzi's operator performs a coordinated rolling restart.

Its inspected
[ClusterRole](https://github.com/strimzi/drain-cleaner/blob/3654e5794fc31e0dd62650c85490b3a2b95fe49e/install/kubernetes/020-ClusterRole.yaml)
grants `get` and `patch` on pods, without Node access. An administrator still
installs the cluster-scoped webhook configuration.

The README also describes a legacy mode that allows the webhook request and
relies on a zero-unavailable PDB to prevent the underlying eviction. The default
denial mode can require retrying `kubectl drain`; compatibility with the actual
maintenance tool should be checked rather than assuming every client retries.

This is not a drop-in generic CNPG integration. It demonstrates the architecture,
but the annotation and lifecycle handling are Strimzi-specific. It also detects
eviction attempts rather than simple cordons or a complete node-wide maintenance
state.

Sources:

- [Drain Cleaner README](https://github.com/strimzi/drain-cleaner/blob/main/README.md)
- [Original project explanation](https://strimzi.io/blog/2021/09/24/drain-cleaner/)

### Rack awareness without a node-reading init container

Strimzi supports `rack.type: environment-variable`, with `envVarName` selecting
the variable holding the rack ID. The administrator supplies its value through
configuration, a webhook, or another mechanism. This avoids the topology-reading
init container and associated ClusterRoleBindings for that function.

The inspected changelog lists this capability under 1.0.0, and the current 1.3.0
documentation describes it. It does not eliminate unrelated Node dependencies,
such as those a particular external-listener configuration may introduce.
Placement rules must also be configured by the user for this mode.

Sources:

- [Strimzi configuration reference](https://strimzi.io/docs/operators/latest/configuring.html#type-EnvironmentVariableRack-reference)
- [EnvironmentVariableRack documentation snapshot](https://github.com/strimzi/strimzi-kafka-operator/blob/610b735ef1b89eaebee28c0cc2b6de18b308aa89/documentation/api/io.strimzi.api.kafka.model.common.EnvironmentVariableRack.adoc)
- [Changelog snapshot](https://github.com/strimzi/strimzi-kafka-operator/blob/610b735ef1b89eaebee28c0cc2b6de18b308aa89/CHANGELOG.md)

An asynchronous NodeEcho update is not enough to guarantee a startup environment
variable contains the projected value. Native binding-time projection or a
separate startup-coordination mechanism would be needed for that guarantee.

## ECK / Elasticsearch

ECK implements Node-to-Pod metadata projection through:

- An operator allowlist, `exposed-node-labels`.
- The Elasticsearch annotation `eck.k8s.elastic.co/downward-node-labels`.
- Selected node labels copied as pod annotations for Downward API consumption.

Its documentation now describes using native Kubernetes 1.35 zone/region pod
labels instead of this extra projection. It explicitly distinguishes pod labels
from the annotations produced by the ECK-specific mechanism.

The inspected Helm templates conditionally add the node-reading RBAC rule when
`config.exposedNodeLabels` is non-empty. That is evidence of configurable access
for this feature, not proof that every possible ECK configuration is free of all
cluster-scoped dependencies.

Sources:

- [Advanced Elasticsearch node scheduling](https://www.elastic.co/docs/deploy-manage/deploy/cloud-on-k8s/advanced-elasticsearch-node-scheduling)
- [Operator configuration flags](https://www.elastic.co/docs/reference/cloud-on-k8s/eck-configuration-flags)
- [Helm ClusterRole snapshot](https://github.com/elastic/cloud-on-k8s/blob/c05102f57d996f7b147ca1dabbfe02ba169729ef/deploy/eck-operator/templates/cluster-roles.yaml)
- [Node RBAC helper snapshot](https://github.com/elastic/cloud-on-k8s/blob/c05102f57d996f7b147ca1dabbfe02ba169729ef/deploy/eck-operator/templates/_helpers.tpl)

## Prometheus Operator / Prometheus

The `AttachMetadata.node` API documents `list` and `watch` permissions on Nodes
for the **Prometheus ServiceAccount**. This is not the same as the operator's
own permissions. Node discovery metadata is exposed as internal
`__meta_kubernetes_node_*` labels and requires relabeling to reach time series.

Projecting selected values onto pods could cover some enrichment use cases with
pod metadata and corresponding relabeling. It would not automatically replace
all node discovery, node-address, or node-health functionality.

Source: [AttachMetadata API source](https://github.com/prometheus-operator/prometheus-operator/blob/main/pkg/apis/monitoring/v1/types.go).

## Kyverno

Kyverno publishes examples for copying node metadata during pod binding and for
asynchronously mutating the existing pod with node labels. These are useful
alternatives when Kyverno is already deployed.

The 2024 article describes historical binding behavior; current Kubernetes
topology-label admission changes must be considered before generalizing its
statements about labels versus annotations.

Sources:

- [Assigning Node Metadata to Pods](https://kyverno.io/blog/2024/02/19/assigning-node-metadata-to-pods/)
- [Add scheduled node labels to a pod](https://github.com/kyverno/policies/blob/main/other/add-node-labels-pod/add-node-labels-pod.yaml)
- [Mutate pod binding](https://kyverno.io/policies/other/mutate-pod-binding/mutate-pod-binding/)

## Other operators

The [operators survey](operators.md) reads the Node usage of about twenty
operators. In short:

- Most PostgreSQL, MySQL, Cassandra, Redis and messaging operators never read
  Nodes (Crunchy PGO, the Percona PostgreSQL/MySQL operators, StackGres,
  cass-operator, Vitess, RabbitMQ and others).
- Those that do read **configurable label keys** for rack or zone awareness
  (ECK, Strimzi, TiDB, Koperator, Redpanda, Percona MongoDB) or a
  **cordon/drain signal** (Zalando: `spec.unschedulable` only; CloudNativePG:
  `spec.unschedulable` plus taints; Redpanda and Rook: unschedulable, taints and
  conditions).
- Some needs are out of reach of a per-pod projection: node addresses for
  NodePort-style listeners (Strimzi, Koperator, Redpanda, Rook), and cluster
  facts such as the distribution, the Node count or the capacity (CockroachDB,
  MinIO, MongoDB telemetry, KubeBlocks, Flink).
- Strimzi and Redpanda grant Node rights to their *operand* pods, so the
  operator must be able to create ClusterRoleBindings; a projection would remove
  that escalation path too.

## Implications for NodeEcho

The research confirms a repeated need to externalize selected node context and
avoid granting each application component its own Node access. It does not
establish an existing universal annotation contract for cordon/drain state.

NodeEcho's proposed distinction is continuous, configurable projection of node
facts for multiple consumers. It should complement native topology admission
and keep application-specific maintenance decisions in the consuming operator.

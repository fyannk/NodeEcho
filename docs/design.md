# Proposed design

Status: **proposal**, 2026-10-16. Examples below are not an implemented API.

## Goal

Let platform administrators publish selected node facts into workload namespaces
so application operators can run without direct Node permissions while retaining
the information needed for their application-specific decisions.

The first consumer is a modified CloudNativePG operator. No upstream integration
or compatibility guarantee exists yet.

## Initial scope

- Reflect `Node.spec.unschedulable`.
- Reflect selected taints, preserving key, value, and effect.
- Copy explicitly allowed node labels and annotations to configured pod keys.
- Target selected namespaces and pods.
- Reconcile assignment, node changes, pod changes, startup, and source deletion.
- Patch only managed metadata keys when their desired value changes.

Keep the first implementation small. A static operator configuration may be
enough initially; a policy CRD should be justified by an actual need for multiple
independently managed rules. An arbitrary expression engine is not required for
the initial CNPG use case.

NodeEcho should not evict pods, change their readiness, control PostgreSQL, or
infer an authoritative drain phase from the number of remaining pods.

## Rules

Proposal (2026-10-12): one key of an administrator-owned ConfigMap holds a list
of rule objects. There is no default consumer: without a rule, NodeEcho does
nothing. A CRD only if adoption calls for it.

```yaml
rules:
- name: cnpg-instances          # required, unique: logs, metrics, field-manager suffix
  namespaceSelector: {}         # label selector on namespaces; empty = all
  podSelector:                  # required, never empty: the pods NodeEcho writes
    matchLabels: {cnpg.io/podRole: instance}
  nodeSelector: {}              # optional: only pods on matching nodes
  unschedulable:                # echo spec.unschedulable; omit the block to skip
    key: nodeecho.fyannk.dev/unschedulable   # default
    target: labels              # or annotations
  taints:                       # echo the listed taints; omit the block to skip
    copy: [node.kubernetes.io/unschedulable, ToBeDeletedByClusterAutoscaler,
           karpenter.sh/disrupted, karpenter.sh/disruption]
    key: nodeecho.fyannk.dev/taints          # default; always an annotation
  labels:
    copy: [topology.kubernetes.io/zone, rack.example.org/id]
    target: labels              # or annotations (ECK reads annotations)
    prefix: ""                  # optional: <prefix><key> for keys that could clash
    forceKubernetesManaged: false
  annotations:                  # node annotations, same fields minus forceKubernetesManaged
    copy: [rack.example.org/position]
    target: annotations         # default; labels only for label-safe values
    prefix: ""
```

- Selection is on pods, since NodeEcho writes pods: `podSelector` is required;
  `namespaceSelector` and `nodeSelector` only narrow it.
- The taint list and the destination keys are per rule, so each consumer
  echoes the taints it needs under the keys it reads.
- An invalid rule (missing name or `podSelector`, duplicate name, bad selector
  or key) is refused with a log line and a metric; valid rules keep running.
  A ConfigMap has no status, so logs and metrics are the feedback channel.
- Node labels and node annotations are separate sources, each with its own
  block, see [Copied annotations](#copied-annotations). Within one rule, two
  copies resolving to the same pod key (a node label and a node annotation of
  the same name, or a `prefix` making them collide) are an invalid rule.
- Two rules on the same pod: copies of the same source agree (same node
  value). The same `unschedulable` or `taints` key in two rules is an invalid
  configuration, so at most one rule keeps each default key.

NodeEcho's own keys use the project domain `nodeecho.fyannk.dev`.
`example.org` keys stand for other parties' keys (a consumer's or an
administrator's).

## Facts and policies

NodeEcho echoes node facts and interprets nothing: it never decides that a
node is draining. Whether a cordon or a taint means maintenance is the
consumer's policy:

| Published fact | Consumer responsibility |
| --- | --- |
| Unschedulable boolean | Decide whether cordon should trigger a switchover |
| Selected taints | Apply the consumer's configured maintenance-taint policy |
| Topology labels | Apply the consumer's failure-domain policy |
| Source identity and freshness information | Decide whether context is usable |

### Unschedulable and taints

Both are plain echoes of the Node spec, written on every selected pod of the
node:

- `unschedulable`: `"true"` while `spec.unschedulable` is true, removed
  otherwise, as the Node itself omits the field. A label by default, so it can
  be selected (`kubectl get pods -l nodeecho.fyannk.dev/unschedulable`).
- `taints`: an annotation holding a JSON array of the node's taints whose key
  is in the rule's `copy` list, each object as in `Node.spec.taints` (`key`,
  `value` when set, `effect`, `timeAdded` when set), in Node order; removed
  when none is present. An annotation, because taint keys contain `/` and the
  array has no label-safe form.

Nothing is written for a schedulable, untainted node, so most pods carry
neither key. The absence of both therefore also covers "not projected yet";
telling the two apart is part of the freshness and unknown-state decision
below.

There is no default taint list and no wildcard: only listed keys are echoed.
The example above is CloudNativePG's default `DRAIN_TAINTS`; a consumer
configured with another list needs the same list in its rule. Pods bound
later get the keys when their node is known, and a full resync runs at
startup.

### Copied labels and the Kubernetes version

- Same key on the pod as on the node by default, as KEP-4742 does: consumers
  configure the key they already know, and the same pod spec works with or
  without NodeEcho. An optional `prefix` avoids clashes with a pod's own keys.
- Labels or annotations per rule: ECK reads annotations, Strimzi, TiDB,
  Percona MongoDB and CloudNativePG labels ([operators survey](operators.md)).
- Values follow later node changes, whereas KEP-4742 copies once at binding;
  this difference is documented.
- On Kubernetes 1.35 and later (version read from `/version`, readable by
  every authenticated client), the keys that Kubernetes copies itself
  (`topology.kubernetes.io/zone` and `/region`, not `kubernetes.io/hostname`)
  are left to Kubernetes by default. `forceKubernetesManaged` copies them
  anyway, for clusters where the feature gate is off or distributions that
  differ. Without it, a selected pod missing such a key while its node has it
  gets a Warning Event and a metric: it signals the flag, it does not copy
  silently. Keys that Kubernetes does not copy are always copied when
  configured.

### Copied annotations

- The `annotations` block copies node annotations, with the same `copy`,
  `target` and `prefix` semantics as `labels`. Consumers that need it:
  Redpanda's `rackAwareness.nodeAnnotation` can name a node annotation, and
  Rook reads mon IPs from one ([operators survey](operators.md)).
- `target` defaults to `annotations`: any node annotation value is valid as a
  pod annotation value. A node annotation value can hold any characters, up
  to the 256 KiB shared by all of an object's annotations, whereas a label
  value is at most 63 characters of `[A-Za-z0-9._-]`, starting and ending
  alphanumeric. With `target: labels`,
  a value that is not a valid label value is not written; the pod gets a
  Warning Event and a metric, and a previously copied value is removed rather
  than left stale.
- Kubernetes does not copy node annotations to pods, so there is no
  `forceKubernetesManaged` here: configured keys are always copied.
- Values follow later node changes, as for labels.

## Illustrative pod metadata

Keys use the project domain `nodeecho.fyannk.dev`. A rule's `key` fields
let an administrator publish under other keys, for a consumer that already
reads its own.

```yaml
metadata:
  labels:
    nodeecho.fyannk.dev/unschedulable: "true"
  annotations:
    nodeecho.fyannk.dev/taints: '[{"key":"karpenter.sh/disrupted","effect":"NoSchedule"}]'
    nodeecho.fyannk.dev/source-node-uid: "<node-uid>"
    nodeecho.fyannk.dev/schema-version: "v1"
```

The taint selection must include every key that the configured consumer needs.
A JSON annotation avoids label-value length and character constraints for
structured data. Topology can be copied as labels under its original or a
configured destination key, subject to collision rules.

The example is deliberately incomplete: freshness and source-availability
semantics must be designed before it becomes a compatibility contract.

## Reconciliation

1. Watch Nodes and the selected pods.
2. Index pods by `spec.nodeName` in the local cache.
3. On a relevant Node change, enqueue only selected pods assigned to that node.
4. On pod assignment or startup, calculate the current projection from the
   cached source; ignore unassigned pods until a node is known.
5. Compare the complete desired managed projection to the existing values.
6. Patch changed managed keys together; retry conflicts and avoid whole-map
   replacement that overwrites another writer's metadata.
7. Reconcile managed-key removal when source keys disappear, a node is
   uncordoned, taints are removed, or policy changes.

Use predicates to ignore irrelevant node updates, including unrelated status
changes. Do not mirror every heartbeat onto every pod.

Guard against replacement objects with the same name. Use appropriate UID or
optimistic-concurrency preconditions when patching pods, and retain source Node
UID so node deletion/recreation is distinguishable.

## Consistency and freshness

The proposed controller is eventually consistent. It cannot guarantee that a
metadata patch happens before a workload starts or an eviction proceeds.

Missing context is not equivalent to `unschedulable=false`. Consumers should
distinguish uninitialized, available, unavailable, and stale context according
to a documented policy.

A last-change timestamp does not prove that a projector is still observing the
source. If consumers require a freshness guarantee, design an explicit bounded
refresh/health mechanism and account for its write amplification. A stopped
controller cannot update pods to declare itself stopped.

For source deletion, define whether topology is retained as historical placement
while availability becomes unknown. Do not silently preserve a healthy-state
claim after source loss.

## Permissions and trust

The platform component still needs cluster-wide Node observation. The benefit
is centralizing that access rather than eliminating it from the cluster.

Expected runtime permissions:

- Nodes: `list/watch`, plus `get` if the implementation performs direct reads.
- Selected pods: `list/watch/patch`, plus `get` for direct reads if needed.
- Leader-election Leases in the component namespace if high availability is used.
- Events `create`, its own ConfigMap `get/watch`, nothing else.

Ordinary Kubernetes RBAC does not restrict a pod `patch` grant to particular
metadata keys or enforce arbitrary label-based object selection. Namespace
RoleBindings can limit where NodeEcho writes; admission policy can additionally
protect a reserved metadata prefix and reject unauthorized changes.

Only publish an explicit allowlist. Node metadata can reveal infrastructure
information to tenants who could not previously read Nodes.

Metadata that influences primary selection must have a defined trust boundary.
The projection is an input to HA decisions, not evidence of fencing or proof
that a process on the source node has terminated.

## Startup-sensitive consumers

CNPG consumes pod metadata in its controller, so it can react to a later update.
An application reading topology through an environment variable is different:
the running process does not automatically receive later metadata changes.

Prefer Kubernetes-native binding-time zone/region projection where available.
For custom startup data, consider a binding-time webhook or explicit startup
coordination only if a real consumer requires it. A normal pod-creation webhook
cannot generally know the node that the scheduler will select later.

## Optional future eviction integration

Strimzi Drain Cleaner is a useful model for an additional eviction-aware
component. A webhook could notify a compatible consumer and defer an eviction
until the application has prepared.

This requires an explicit protocol, timeout/retry behavior, dry-run handling,
webhook side-effect declaration, and compatibility with the maintenance client.
Requests may be denied by other checks or abandoned, so a notification is not
proof an eviction completed. Direct deletes can bypass the eviction path.

This is outside the proposed first release. It also does not replace the cordon
signal needed to reproduce CNPG's current proactive behavior.

## Performance

One shared Node cache can replace duplicated Node observation in several
operators, but pod projection creates additional writes and watch events.
Net performance improvement is not guaranteed.

For a relevant node transition with P selected pods, expect up to P pod patches
before retries. Patch only semantic changes, bound worker concurrency, use
backoff, and expose queue/propagation-latency/error metrics. Benchmark the full
path including consumer reconciliations before making scalability claims.

## Validation before claiming CNPG parity

Use end-to-end scenarios against the exact supported CNPG/fork and Kubernetes
versions, with a direct-Node deployment as the reference:

- Cordon and uncordon the primary's node.
- Add/remove each configured maintenance taint, including custom keys.
- Cordon multiple nodes and ensure an unsuitable replica is not selected.
- Drain the primary's node while its PDB protects it; observe switchover and
  drain completion without weakening the existing PostgreSQL checks.
- Exercise local-PV maintenance and `reusePVC` behavior separately.
- Verify native and custom topology, missing labels, and label changes.
- Start CNPG before the first projection; restart or interrupt NodeEcho.
- Delete/recreate source Nodes or destination pods with the same names.
- Exercise patch conflicts, metadata collisions, policy changes, and cleanup.
- Measure propagation latency and API write volume during maintenance bursts.

Information parity after convergence, timing differences, and behavior under
projector failure should be reported separately.

## Outside a per-pod projection

- Node addresses (`status.addresses` by type) for NodePort-style listeners
  (Strimzi, Koperator, Redpanda, Rook). They could be published as per-type
  annotations; until then those listeners keep needing Node rights.
- Cluster facts that are not per pod: distribution, Node count, capacity
  (CockroachDB, MinIO, KubeBlocks, Flink). Most are optional or have an
  override; see the [operators survey](operators.md).

## Open decisions


- Implementation language and Kubernetes client framework.
- Configuration format and whether a CRD is warranted.
- Schema versioning of the metadata.
- Freshness, unknown-state, and deleted-source behavior.
- Default collision and managed-key cleanup policy.
- Supported CNPG integration/version strategy.
- Whether a namespaced per-node context object is preferable to pod patches
  for consumers that do not need Downward API access.

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
  drain:
    enabled: true
    domain: drain.example.org   # prefix of this rule's drain mark
    taints: []                  # empty = default list, see "Drain mark"
  labels:
    copy: [topology.kubernetes.io/zone, rack.example.org/id]
    target: labels              # or annotations (ECK reads annotations)
    prefix: ""                  # optional: <prefix><key> for keys that could clash
    forceKubernetesManaged: false
```

- Selection is on pods, since NodeEcho writes pods: `podSelector` is required;
  `namespaceSelector` and `nodeSelector` only narrow it.
- The drain domain and taint list are per rule, so each consumer reads the key
  it configured and gets exactly its own maintenance policy; no shared naming
  has to be agreed between consumers.
- An invalid rule (missing name or `podSelector`, duplicate name, bad selector
  or domain) is refused with a log line and a metric; valid rules keep running.
  A ConfigMap has no status, so logs and metrics are the feedback channel.
- Two rules on the same pod: label copies agree (same node value); drain marks
  under different domains are both written; the same domain in two rules is
  an invalid configuration.

`example.org` keys are placeholders, as below.

## Facts and policies

Publish node facts rather than a single universal `draining=true` decision:

| Published fact | Consumer responsibility |
| --- | --- |
| Unschedulable boolean | Decide whether cordon should trigger a switchover |
| Selected taints | Apply the consumer's configured maintenance-taint policy |
| Topology labels | Apply the consumer's failure-domain policy |
| Source identity and freshness information | Decide whether context is usable |

Different CNPG installations can configure different `DRAIN_TAINTS`; embedding
one fixed list in NodeEcho would couple the projection to one consumer policy.
The rules above avoid that coupling with a per-rule taint list, so NodeEcho
can also publish a consumer-specific drain mark next to the facts.

### Drain mark

For each rule with `drain.enabled`, on a node that is draining by that rule:

- label `<domain>/node-draining: "true"` on every selected pod of the node,
  removed (not set to `"false"`) when the node is schedulable again. A label,
  so it can be selected (`kubectl get pods -l <domain>/node-draining`) and
  watched with a label selector;
- annotations `<domain>/node-draining-since` (first observation, RFC 3339) and
  `<domain>/node-draining-reason` (`cordon` or `taint:<key>`; the `:` is why
  these are annotations).

Draining = `spec.unschedulable`, or a taint key in the rule's list. The default
list is CloudNativePG's: `node.kubernetes.io/unschedulable`,
`ToBeDeletedByClusterAutoscaler`, `karpenter.sh/disrupted`,
`karpenter.sh/disruption`. Pods bound later to a draining node are marked too,
and a full resync runs at startup. A consumer computing its own decision from
the published facts can ignore the mark.

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

## Illustrative pod metadata

`nodeinfo.example.org` is a placeholder, not a chosen or registered project
domain. The final contract must use an administrator-controlled prefix.

```yaml
metadata:
  labels:
    nodeinfo.example.org/unschedulable: "true"
  annotations:
    nodeinfo.example.org/taints: '[{"key":"karpenter.sh/disrupted","effect":"NoSchedule"}]'
    nodeinfo.example.org/source-node-uid: "<node-uid>"
    nodeinfo.example.org/schema-version: "v1"
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

- Project name domain for the metadata keys (placeholder `example.org`).
- Drain output: the per-rule mark of the 2026-10-12 proposal, the raw facts of
  this document, or both.

- Implementation language and Kubernetes client framework.
- Configuration format and whether a CRD is warranted.
- Schema versioning of the metadata.
- Freshness, unknown-state, and deleted-source behavior.
- Default collision and managed-key cleanup policy.
- Supported CNPG integration/version strategy.
- Whether a namespaced per-node context object is preferable to pod patches
  for consumers that do not need Downward API access.

---
sidebar_position: 1
title: CloudNativePG
---

# CloudNativePG: removing direct Node access while retaining behavior

Research checked: **2026-10-16**.

## Scope and evidence

The motivating scenario is a local CloudNativePG fork that no longer requires
Node access. That fork was not available for inspection. This document analyzes
upstream source and proposes integration work; it is not a parity audit of the
fork.

The source snapshot inspected is CloudNativePG commit
[`89e5adb773dac7fc00eff1a73f74795c37973c67`](https://github.com/cloudnative-pg/cloudnative-pg/tree/89e5adb773dac7fc00eff1a73f74795c37973c67).
It is a development snapshot, not a claim that all described APIs are available
in every released version.

## What CNPG reads from Nodes

| Concern | Verified source behavior | Information needed from a replacement |
| --- | --- | --- |
| Proactive switchover | A primary on a node considered unschedulable/draining can trigger a planned switchover, subject to the existing eligibility checks | Cordon state and configured drain-taint presence |
| Switchover candidate selection | Candidates on nodes considered unschedulable/draining are skipped | The same context on every candidate pod, not just the current primary |
| Replication topology | Node labels can define failure domains for synchronous replication | Configured topology label values |
| Reconciliation trigger | Node events enqueue relevant clusters | A corresponding pod/context update that the modified operator observes |
| Resource collection | `getManagedResources` calls `getNodes`, which lists Nodes | Remove or bypass that load, even when topology comes from pods |

Sources:

- [`replicas.go`](https://github.com/cloudnative-pg/cloudnative-pg/blob/89e5adb773dac7fc00eff1a73f74795c37973c67/internal/controller/replicas.go)
- [`cluster_status.go`](https://github.com/cloudnative-pg/cloudnative-pg/blob/89e5adb773dac7fc00eff1a73f74795c37973c67/internal/controller/cluster_status.go)
- [`cluster_controller.go`](https://github.com/cloudnative-pg/cloudnative-pg/blob/89e5adb773dac7fc00eff1a73f74795c37973c67/internal/controller/cluster_controller.go)
- [`cluster_predicates.go`](https://github.com/cloudnative-pg/cloudnative-pg/blob/89e5adb773dac7fc00eff1a73f74795c37973c67/internal/controller/cluster_predicates.go)

### Exact Node data read

Checked in the inspected source; the list is short and closed:

1. `spec.unschedulable`.
2. `spec.taints[].key`, compared with `DRAIN_TAINTS`
   ([`replicas.go`](https://github.com/cloudnative-pg/cloudnative-pg/blob/89e5adb773dac7fc00eff1a73f74795c37973c67/internal/controller/replicas.go#L254-L279)).
3. `metadata.labels`, only for the keys listed in the Cluster, through
   `spec.postgresql.synchronous.nodeFailureDomainKeys` or the legacy
   `spec.postgresql.syncReplicaElectionConstraint.nodeLabelsAntiAffinity`
   ([`cluster_status.go`](https://github.com/cloudnative-pg/cloudnative-pg/blob/89e5adb773dac7fc00eff1a73f74795c37973c67/internal/controller/cluster_status.go#L287-L296)).
   No key is fixed in the code; typical values are `topology.kubernetes.io/zone`
   and `kubernetes.io/hostname`.
4. The node name, which comes from `pod.spec.nodeName`, not from a Node read.

No Node annotation is ever read, and the instance manager (the process inside the
PostgreSQL pods) never reads Nodes. `getManagedResources` lists every Node at
each reconciliation only to feed item 3 and the node count of
`status.topology.nodesUsed`
([`cluster_status.go`](https://github.com/cloudnative-pg/cloudnative-pg/blob/89e5adb773dac7fc00eff1a73f74795c37973c67/internal/controller/cluster_status.go#L163-L188)),
which can be counted from the instance pods' `spec.nodeName` instead.

### The maintenance predicate is broader than cordon

In the inspected source, `isNodeUnschedulableOrBeingDrained` returns true when:

1. Any node taint has a key in the configured `DRAIN_TAINTS` list; or
2. `node.spec.unschedulable` is true.

The predicate matches taint keys; it does not additionally filter taint values
or effects. A compatibility implementation should preserve this behavior rather
than silently interpreting all `NoSchedule` taints as maintenance.

The documented default keys are:

- `node.kubernetes.io/unschedulable`
- `ToBeDeletedByClusterAutoscaler`
- `karpenter.sh/disrupted`
- `karpenter.sh/disruption`

Source: [operator configuration](https://cloudnative-pg.io/docs/1.26/operator_conf/).
Copying only a cordon boolean would omit autoscaler/Karpenter signals.

### PDB coordination

CNPG protects the primary with a PodDisruptionBudget and can move the primary
role away from a node being maintained. Removing node observation without an
alternative signal can therefore leave a drain waiting on the protected primary.

This is a coordination problem. It does not mean CNPG needs cluster-wide
permission to create an eviction request for a namespaced pod. Removing PDBs is
not a behavior-preserving substitute for restoring the maintenance signal.

Sources:

- [Kubernetes upgrade and maintenance](https://cloudnative-pg.io/docs/1.27/kubernetes_upgrade/)
- [Issue #7799: cordon causes a switchover](https://github.com/cloudnative-pg/cloudnative-pg/issues/7799)

The second source also demonstrates a semantic distinction: CNPG's existing
behavior can react to a simple cordon, before any eviction is requested. A pure
eviction webhook would not reproduce that trigger.

## Upstream optional-watch work

[PR #10135: conditionally watch node resources](https://github.com/cloudnative-pg/cloudnative-pg/pull/10135)
was **open and not merged** when checked on 2026-10-16. It proposes a
`WATCH_NODES` setting and avoids both watching Nodes and reading node information
when disabled. Its description identifies it as a split/modification of PR
#9350 and associates it with [issue #9346](https://github.com/cloudnative-pg/cloudnative-pg/issues/9346).

Issue #9346 requests optional access to Nodes and ClusterImageCatalog resources
for hardened environments where startup currently fails without those rights.

The proposed optional-watch mode documents functionality losses; it does not
implement a NodeEcho-style projection to recover them.

A second proposal, `API_NODE_ENABLED`, is prepared outside the upstream
repository as part of a wider "optional APIs" change (PodMonitor,
VolumeSnapshot, ClusterImageCatalog and Node), and has not been submitted
upstream yet. Its Node part, checked on its current branch:

- Default `true`; the Node API is probed at startup, cluster-wide, like the
  other optional APIs. When disabled, the operator neither watches nor lists
  Nodes, and the `nodes` rules can be removed from its `ClusterRole`.
- Drained or cordoned nodes are not detected: the primary of a node being
  drained is evicted instead of being switched over first, and a failover
  follows.
- `podFailureDomainKeys` keeps working. When the topology needs node labels
  (`nodeFailureDomainKeys` or `nodeLabelsAntiAffinity`), the Cluster carries a
  `TopologyUnavailable` condition and a Warning Event says once that those
  constraints are not applied.
- `status.topology.nodesUsed` is still counted, from the instance pods.
- The instance manager no longer requeues every 30 seconds for a topology
  that cannot be extracted.

Like `WATCH_NODES`, it documents the losses without recovering them; NodeEcho
is the proposed recovery. `WATCH_NODES` must not be
presented as an available upstream release setting solely because this PR exists.

## Pod-based failure domains

The inspected development source and
[development replication documentation](https://cloudnative-pg.io/docs/devel/replication/)
provide mutually exclusive options under `spec.postgresql.synchronous`:

- `podFailureDomainKeys`: resolve domain values exclusively from pod labels.
- `nodeFailureDomainKeys`: resolve domain values from the hosting node's labels.

Example for versions that implement this API:

```yaml
spec:
  postgresql:
    synchronous:
      method: any
      number: 1
      podFailureDomainKeys:
        - topology.kubernetes.io/zone
```

This is an upstream precedent for consuming pod-projected context. It does not
by itself remove the controller's Node watch or unconditional Node collection.

Missing topology matters: the development documentation says that if topology
cannot be extracted or there are insufficient cross-domain standbys, the domain
constraint is not applied. A projection that arrives after reconciliation can
temporarily cause that fallback. NodeEcho must not promise strict domain
enforcement merely by copying labels asynchronously.

## Proposed integration boundary

Keep CNPG's PostgreSQL decisions in CNPG. NodeEcho should publish node facts;
the modified CNPG should interpret those facts using its existing configuration.

Required work in the consumer:

1. Replace maintenance-state Node reads for the primary and all candidates.
2. Remove/bypass `getNodes` and the Node watch in the selected mode.
3. Read topology from pod labels, with version-appropriate handling of the
   legacy `syncReplicaElectionConstraint` API if needed.
4. Ensure metadata-only pod updates cause reconciliation. The inspected
   controller declares `Owns(&corev1.Pod{})` without any predicate
   ([`cluster_controller.go`](https://github.com/cloudnative-pg/cloudnative-pg/blob/89e5adb773dac7fc00eff1a73f74795c37973c67/internal/controller/cluster_controller.go#L1338)),
   so any metadata change on an instance pod already enqueues its Cluster; the
   `API_NODE_ENABLED` proposal keeps that declaration unchanged. The wake-up
   that the Node watch provides today therefore needs no new watch. Other
   modified operators need their own verification.
5. Define explicit behavior for missing, stale, deleted-node, or invalid context.
6. Preserve all existing PostgreSQL health, replication, PDB, and switchover checks.

A source-interface abstraction in CNPG could separate direct Node observation
from pod-projected context. This is a design suggestion, not an existing API.

### Smallest consumer change

The drain side needs only an alternative source for the predicate above:
evaluate it on the `unschedulable` and `taints` keys that NodeEcho echoes on
the instance pod (the primary and every switchover candidate) instead of on
the Node, for example through a setting naming those keys
(`NODE_INFO_PREFIX` is a placeholder). CNPG keeps its `DRAIN_TAINTS` policy;
the NodeEcho rule must echo at least those taint keys.

The topology side needs no new code: copy the node labels onto the instance
pods under the same keys and use `podFailureDomainKeys`, which the development
documentation already recommends. Clusters using `nodeFailureDomainKeys` or the
legacy `nodeLabelsAntiAffinity` move to it.

## Limits of the equivalence claim

A projection can restore the information used by these paths, but it adds
another asynchronous hop. Equivalent decisions after convergence do not imply
identical reaction time or ordering relative to an eviction.

Neither a cordon label nor a maintenance annotation proves that the old primary
has stopped. NodeEcho must not replace fencing, quorum, lease, or PostgreSQL
safety mechanisms.

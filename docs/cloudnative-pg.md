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
implement a NodeEcho-style projection to recover them. `WATCH_NODES` must not be
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
   controller already declares `Owns(&corev1.Pod{})`, but fork predicates and
   subsequent changes need verification.
5. Define explicit behavior for missing, stale, deleted-node, or invalid context.
6. Preserve all existing PostgreSQL health, replication, PDB, and switchover checks.

A source-interface abstraction in CNPG could separate direct Node observation
from pod-projected context. This is a design suggestion, not an existing API.

## Limits of the equivalence claim

A projection can restore the information used by these paths, but it adds
another asynchronous hop. Equivalent decisions after convergence do not imply
identical reaction time or ordering relative to an eviction.

Neither a cordon label nor a maintenance annotation proves that the old primary
has stopped. NodeEcho must not replace fencing, quorum, lease, or PostgreSQL
safety mechanisms.

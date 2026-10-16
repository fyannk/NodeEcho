---
sidebar_position: 4
title: Sizing
---

# Sizing

NodeEcho caches every pod, Node and Namespace of the cluster, stripped to
what it reads: names, labels, annotations, the pod's node name, the Node's
cordon and taints, and its own field manager entries. Its memory grows with
the number of pods in the cluster, selected or not.

## Measured

With `hack/scale.sh` (`make scale`): Kubernetes 1.36, fake Nodes, half of
the pods selected by a rule.

| Pods | Steady RSS | Peak RSS (rule change) | Cordon of every node echoed |
| --- | --- | --- | --- |
| 2,000 on 20 nodes | 45 MiB | 52 MiB | 4 s for 1,000 pods |
| 20,000 on 200 nodes | 117 MiB | 162 MiB | 18 s for 10,000 pods |

- One write per pod that needs a change, and none for the others. A restart
  writes nothing.
- A rule change reconciles every pod: about 50 s at 20,000 pods.
- One node with about 100 pods is echoed in about 0.2 s.

The fake pods are small. Pods with many labels or large annotations take
more memory: annotations are kept whole.

## Defaults

```yaml
resources:
  requests: {cpu: 10m, memory: 128Mi}
  limits: {memory: 512Mi}
env:
  - name: GOMEMLIMIT
    value: 450MiB
```

`GOMEMLIMIT` makes the Go runtime collect harder before reaching the limit.
Keep it at about 90% of `limits.memory` if you change the limit. Above about
50,000 pods, measure on your cluster and raise both.

## Measure your own

```bash
SCALE_PODS=50000 SCALE_NODES=500 make scale
```

The script uses a throwaway k3d cluster with its own kubeconfig. Memory is
reported from NodeEcho's own metrics (`process_resident_memory_bytes`,
`go_memstats_heap_inuse_bytes`), and API requests from
`rest_client_requests_total`.

---
sidebar_position: 5
title: Sizing
---

# Sizing

NodeEcho caches every pod, Node and Namespace of the cluster, stripped to
what it reads:

- pods: name, labels, node name, NodeEcho's own field manager entries, and
  only the annotations the rules write or NodeEcho owns;
- Nodes: labels, annotations, cordon and taints;
- Namespaces: labels.

Its memory still grows with the number of pods in the cluster, selected or
not, but no longer with their annotations, which are usually the biggest
part of a pod object.

## Measured

With `hack/scale.sh` (`make scale`): Kubernetes 1.36, 20,000 pods on 200
fake Nodes, half of them selected by a rule, each pod carrying a 1 KiB
annotation as tools and controllers leave on real pods.

| | Steady | After cordon and taint | Peak, rule change | After a restart |
| --- | --- | --- | --- | --- |
| RSS | 131 MiB | 153 MiB | 186 MiB | 171 MiB |
| Heap in use | 65 MiB | 105 MiB | 122 MiB | 83 MiB |

Without the annotation filter, the same run needed 175 MiB at steady state
(heap 130 MiB) and peaked at 224 MiB.

- One write per pod that needs a change, and none for the others. A restart
  writes nothing.
- Cordoning all 200 nodes at once reaches the 10,000 selected pods in 20 s;
  one node with about 100 pods is echoed in about 0.2 s.
- A rule change reconciles every pod: about 50 s at 20,000 pods. Pods are
  reconciled one at a time, in one queue: a cordon that arrives during that
  time waits its turn behind the pods already queued.

## More than one pod at a time

`--pod-workers` sets how many pods NodeEcho reconciles at the same time; the
default is one. More workers shorten what touches many pods at once, and
send the API server as many requests at the same time. They do not change
how many writes are made, nor the memory.

One run each of `make scale` at its default size, 5,000 pods on 50 fake
Nodes, half of them selected, on a single-node k3d cluster:

| | 1 worker | 4 workers |
| --- | --- | --- |
| First echo on 2,500 pods | 7.6 s | 1.1 s |
| Cordon of every node echoed | 3.0 s | 0.3 s |
| Rule added, every pod reconciled | 11.5 s | 4.0 s |
| Writes over the whole run | 12,500 | 12,500 |
| RSS after the rule change | 93 MiB | 90 MiB |

These are single runs on an idle API server with no other client, so read
them as an order of magnitude. On a busy cluster the API server's priority
and fairness decides how much of this NodeEcho gets, and NodeEcho does not
limit its own request rate. Raise the number when a rule change or a large
cordon takes too long, and watch the API server while you do:
`SCALE_WORKERS=4 make scale` measures it.

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

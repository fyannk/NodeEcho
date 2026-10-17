---
sidebar_position: 2
title: Configuration
---

# Configuration

Rules live in the ConfigMap `nodeecho`, key `rules.yaml`, in NodeEcho's
namespace. NodeEcho watches it: a change applies within seconds, to every
pod.

## A rule

```yaml
version: 1
rules:
- name: cnpg-instances            # required, unique, a DNS-1123 label
  namespaceSelector: {}           # optional; empty = every namespace
  podSelector:                    # required, never empty
    matchLabels: {cnpg.io/podRole: instance}
  nodeSelector: {}                # optional; empty = every Node
  unschedulable: {}               # echo spec.unschedulable
  taints:                         # echo the taints whose key is listed
    copy: [node.kubernetes.io/unschedulable, ToBeDeletedByClusterAutoscaler, karpenter.sh/disrupted]
  labels:                         # copy Node labels
    copy: [topology.kubernetes.io/zone, rack.example.org/id]
  annotations:                    # copy Node annotations
    copy: [rack.example.org/position]
```

Each block is optional, but a rule needs at least one. Every field is in the
[contract](../reference/contract.md#configuration).

## Blocks and their options

| Block | Field | Default | Meaning |
| --- | --- | --- | --- |
| `unschedulable` | `key` | `nodeecho.fyannk.dev/unschedulable` | pod key |
| | `target` | `labels` | `labels` or `annotations` |
| `taints` | `copy` | required | taint keys to echo |
| | `key` | `nodeecho.fyannk.dev/taints` | pod annotation |
| `labels` | `copy` | required | Node label keys |
| | `target` | `labels` | where they go on the pod |
| | `prefix` | `""` | prepended to each key |
| `annotations` | `copy` | required | Node annotation keys |
| | `target` | `annotations` | where they go on the pod |
| | `prefix` | `""` | prepended to each key |
| `eviction` | `key` | `nodeecho.fyannk.dev/eviction-requested` | pod annotation recording the first eviction request, with the [eviction webhook](eviction-webhook.md) |
| | `response` | `allow` | `allow` or `deny` (HTTP 429) |
| | `message` | names the rule | the refusal's message, with `deny` |

## Examples

**Zone as an annotation**, for a consumer that reads annotations, as ECK
does:

```yaml
- name: eck-zone
  podSelector: {matchLabels: {common.k8s.elastic.co/type: elasticsearch}}
  labels:
    copy: [topology.kubernetes.io/zone]
    target: annotations
```

**Custom rack labels with a prefix**, so they cannot clash with the pod's own
labels:

```yaml
- name: racks
  podSelector: {matchLabels: {app: kafka}}
  labels:
    copy: [rack.example.org/id]
    prefix: node.            # pod label node.rack.example.org/id
```

**Only some nodes**, for instance a database pool:

```yaml
- name: db-pool
  podSelector: {matchLabels: {app: postgres}}
  nodeSelector: {matchLabels: {pool: db}}
  unschedulable: {}
```

**Your own key names**, for a consumer that already reads its own:

```yaml
- name: legacy
  podSelector: {matchLabels: {app: legacy}}
  unschedulable: {key: legacy.example.org/node-cordoned, target: annotations}
```

## Zone and region

From Kubernetes 1.35, `PodTopologyLabelsAdmission` copies
`topology.kubernetes.io/zone` and `topology.kubernetes.io/region` onto pods
at binding. A rule listing them needs no special case: a pod that already
carries the Node's value from Kubernetes is left alone, and a pod that does
not, on an older cluster or with the feature off, gets them from NodeEcho.
After a cluster upgrade, the pods NodeEcho labelled keep its labels and new
pods get Kubernetes'; nothing to restart or reconfigure.

Kubernetes copies once, at binding; NodeEcho follows later Node changes only
for the keys it wrote itself. Copied with a prefix or as annotations, they
are keys Kubernetes does not write, so NodeEcho always copies them.

## What is refused

A refused rule is logged and counted; the others keep running.

- Missing, duplicated or invalid `name`; missing or empty `podSelector`.
- No block, or an empty `copy`, or a key listed twice.
- An invalid selector or key, including a prefix that makes the key invalid.
- Two keys of the rule landing on the same pod key.
- An `unschedulable.key`, `taints.key` or `eviction.key` that another rule
  also uses, as such a key or as a copy destination: both rules are refused.
- An `eviction.response` other than `allow` or `deny`, or an
  `eviction.message` with `allow`.

A refused rule that was already running keeps its last valid version, so a
typo does not strip its keys from every pod.

The whole document is refused, and the last valid configuration kept, for an
unknown `version`, a missing `rules` field, an unknown field or a value of
the wrong type, or a missing ConfigMap. To stop writing, set `rules: []`.

## Admission policies

When the [writer-scope policy](admission-policies.md) is installed, every
destination key of every rule must be on its allowed list; the Helm chart
computes the list from the rules in its values. A key outside it
makes the rule's write on that pod fail, reported as a `KeySkipped` Warning
Event.

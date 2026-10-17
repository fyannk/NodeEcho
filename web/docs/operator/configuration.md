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
| | `forceKubernetesManaged` | `false` | copy zone and region even on 1.35+ |
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

From Kubernetes 1.35, Kubernetes copies `topology.kubernetes.io/zone` and
`topology.kubernetes.io/region` onto pods at binding. A rule copying them as
plain labels leaves them to Kubernetes; a pod that should have them but does
not gets a `KubernetesTopologyMissing` Warning Event. On a cluster where
`PodTopologyLabelsAdmission` is turned off, set
`labels.forceKubernetesManaged: true` and NodeEcho copies them.

Copied with a prefix or as annotations, they are keys Kubernetes does not
write, so NodeEcho copies them in any case. The Kubernetes version is read at
start: restart NodeEcho after upgrading the cluster past 1.35.

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

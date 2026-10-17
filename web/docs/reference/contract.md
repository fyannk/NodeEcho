---
sidebar_position: 1
title: Contract v1
---

# Contract v1

Status: **draft**, 2026-10-16. No implementation exists yet. This page lists
what a consumer can observe on its pods and what it may rely on; the
[design notes](../design/design.md) keep the reasoning.

## Principles

- NodeEcho echoes Node data onto pods as it is. It interprets nothing: it
  never decides that a node is draining, healthy or ready.
- It writes a key only when there is something to echo. A healthy pod on a
  schedulable, untainted node carries only the copies its rule asks for.
- It writes only what a rule configures. Without a rule, it writes nothing.

## Pods in scope

A pod is in scope for a rule when all of these hold:

- its namespace matches `namespaceSelector` (empty = all namespaces);
- it matches `podSelector` (required, never empty);
- `spec.nodeName` is set, and that Node matches `nodeSelector` (empty = all).

Pods remain in scope until they are deleted, whatever their phase. A pod
whose Node no longer exists is left as it is: the pod garbage collector
deletes it. When a pod
leaves a rule's scope (its labels, its namespace's labels, the Node's labels
or the rule change), NodeEcho deletes the keys that rule wrote on it.

## Keys

`<key>` and `<prefix>` are the rule's settings; defaults are shown.

| Key on the pod | Written as | Value | Present when |
|---|---|---|---|
| `unschedulable.key` (default `nodeecho.fyannk.dev/unschedulable`) | label (default) or annotation | `"true"` | the Node has `spec.unschedulable: true` |
| `taints.key` (default `nodeecho.fyannk.dev/taints`) | annotation | JSON array, see [Taints](#taints) | the Node has at least one taint whose key is in `taints.copy` |
| `<prefix><node label key>` | label (default) or annotation | the Node label value, unchanged | the Node has that label and it is in `labels.copy` |
| `<prefix><node annotation key>` | annotation (default) or label | the Node annotation value, unchanged | the Node has that annotation and it is in `annotations.copy` |

When the condition stops holding, the key is **removed**. NodeEcho never
writes `"false"`, an empty array or a placeholder value. An empty value on the
Node is copied as an empty value.

There is no other key: no version, source-identity, timestamp or heartbeat
key on pods.

### Taints

- Compact JSON array of the Node's taints whose `key` is in `taints.copy`, in
  the order of `Node.spec.taints`.
- Each element is the Kubernetes JSON form of `corev1.Taint`: `key`,
  `value` (omitted when empty), `effect`, `timeAdded` (RFC 3339, omitted when
  unset).
- A key listed once matches every taint with that key, whatever its value or
  effect; two taints with the same key and different effects both appear.

```json
[{"key":"karpenter.sh/disrupted","effect":"NoSchedule"}]
```

### Copied labels and annotations

- The destination key is `<prefix><source key>`; `prefix` defaults to empty,
  so the pod uses the Node's key, as Kubernetes' own zone/region copy does.
- Values follow later Node changes. Kubernetes' own copy is made once at
  binding.
- A value written as a label must be a valid label value (at most 63
  characters of `[A-Za-z0-9._-]`, starting and ending alphanumeric, or empty).
  An invalid value is not written, a previously written value is removed, and
  NodeEcho emits a Warning Event on the pod and increments a metric.
- On Kubernetes 1.35 and later, `topology.kubernetes.io/zone` and
  `topology.kubernetes.io/region` copied as labels without a prefix are left
  to Kubernetes unless the rule sets `labels.forceKubernetesManaged: true`.
  A selected pod missing such a key while its Node has it gets a Warning
  Event. Copied with a prefix or as annotations, they are pod keys Kubernetes
  does not write, so NodeEcho copies them. `kubernetes.io/hostname` and every
  other key are copied whenever configured.

### Eviction requests

Only with the optional eviction webhook installed, and for the pods a rule
with an `eviction` block selects:

| Key (default) | Kind | Value |
| --- | --- | --- |
| `nodeecho.fyannk.dev/eviction-requested` | annotation | when NodeEcho received the first eviction request for the pod, RFC 3339 in UTC, for instance `2026-10-16T09:30:00Z` |

- NodeEcho records the request it receives: a `kubectl drain`, a cluster
  autoscaler or Karpenter scale-down, any client of the eviction API. Later
  requests, such as the retries of a drain, leave the first time in place.
- The key stays until the pod is deleted: neither the end of a drain nor a
  rule change removes it. A pod is evicted once in its life, and a replaced
  pod is a new object without the key.
- A dry-run eviction (`?dryRun=All`) records nothing.
- `eviction.response: allow` (the default) lets the eviction through;
  `deny` refuses it with HTTP 429 and `eviction.message`, as a
  PodDisruptionBudget does, so `kubectl drain` and the autoscalers retry it.
  The request is recorded either way.
- When NodeEcho is down or has no configuration loaded yet, every eviction
  goes through (`failurePolicy: Ignore`).
- The key is written with the field manager `nodeecho-eviction/<rule name>`.

## Ownership

- NodeEcho writes with server-side apply, field manager `nodeecho/<rule name>`.
  It owns only the keys it wrote.
- It never overwrites a key owned by another writer, including a value set in
  the pod's template: the key is skipped, with a Warning Event on the pod and
  a metric. A key another writer removes is written again while the Node
  still has the value.
- A write the API server rejects, for instance annotations over the size
  limit, is skipped with a Warning Event and a metric, and retried on the next
  change to the pod, its Node or the rules.
- Patches carry the pod UID as a precondition, so a pod recreated under the
  same name is never patched with its predecessor's values.
- A pod never changes node (`spec.nodeName` is immutable). A rescheduled pod is
  a new object that starts without the keys and receives those of its new
  node after binding.

## Timing and absence

- Updates are eventually consistent. There is no ordering guarantee relative
  to container start, to an eviction, or to any consumer's reconciliation.
- An absent key means "not set on the Node", or "not projected yet": right
  after binding, while NodeEcho is starting, or while it is down.
- A stopped NodeEcho leaves the last values in place.
- A value read through the Downward API as an environment variable is resolved
  at container start and can be missing; read it from the pod object or a
  Downward API volume instead.

Consumers therefore must not treat an absent key as proof that a node is
schedulable or untainted when that matters, and must keep working, with
reduced behavior, when NodeEcho is not installed.

## Configuration

A ConfigMap in NodeEcho's namespace, default name `nodeecho`, key
`rules.yaml`:

```yaml
version: 1
rules:
- name: cnpg-instances              # required, unique
  namespaceSelector: {}             # optional
  podSelector:                      # required, never empty
    matchLabels: {cnpg.io/podRole: instance}
  nodeSelector: {}                  # optional
  unschedulable:                    # optional block
    key: nodeecho.fyannk.dev/unschedulable
    target: labels                  # labels | annotations
  taints:                           # optional block
    copy: [node.kubernetes.io/unschedulable, ToBeDeletedByClusterAutoscaler,
           karpenter.sh/disrupted, karpenter.sh/disruption]
    key: nodeecho.fyannk.dev/taints
  labels:                           # optional block
    copy: [topology.kubernetes.io/zone]
    target: labels                  # labels | annotations
    prefix: ""
    forceKubernetesManaged: false
  annotations:                      # optional block
    copy: []
    target: annotations             # annotations | labels
    prefix: ""
  eviction:                         # optional block, with the eviction webhook
    key: nodeecho.fyannk.dev/eviction-requested
    response: allow                 # allow | deny
    message: ""                     # with deny; a default names the rule
```

A rule is refused, with a log line and a metric, while the other rules keep
running, when:

- `name` is missing, duplicated or not a DNS-1123 label, or `podSelector` is
  missing or empty;
- it has no `unschedulable`, `taints`, `labels`, `annotations` or `eviction`
  block, or a block's `copy` list is empty or repeats a key;
- `eviction.response` is neither `allow` nor `deny`, or `eviction.message` is
  set with `allow`;
- a selector or a destination key is invalid;
- two of its keys resolve to the same pod key;
- its `unschedulable.key`, `taints.key` or `eviction.key` is also used by
  another rule, as such a key or as a copy destination in the same metadata
  map (both rules are refused).

A refused rule whose name was running keeps its last valid version, so that a
typo does not remove its keys from every pod, unless a valid rule now uses its
`unschedulable.key`, `taints.key` or `eviction.key`.

An unknown `version`, a missing `rules` field, a document that does not
parse (including an unknown field or a value of the wrong type in any rule),
or a missing ConfigMap or key refuses the whole configuration, and NodeEcho
keeps the last valid one. To stop writing, set `rules: []`. Until a first
valid configuration is loaded, NodeEcho writes and removes nothing, and the
elected instance reports not ready; standby instances report ready.

The Kubernetes version, which decides whether zone and region are left to
Kubernetes, is read at start: restart NodeEcho after upgrading the cluster
past 1.35.

## Protection

Pod `patch` rights cannot be limited to some keys. NodeEcho ships two
optional `ValidatingAdmissionPolicy` objects, in `config/policy`, for both
directions:

- `nodeecho-reserved-keys`: anyone able to edit a pod could forge an echoed
  value. The policy rejects creating, changing or removing
  `nodeecho.fyannk.dev/*` labels and annotations from any identity other than
  NodeEcho's ServiceAccount.
- `nodeecho-writer-scope`: NodeEcho's own `patch` right would let it rewrite
  any pod, images included, or change the labels Services and
  NetworkPolicies select on. The policy limits its ServiceAccount to updating
  pods, never their spec, owner references or finalizers, and to changing
  only the labels and annotations listed in the policy. A write outside the
  list is rejected, and NodeEcho reports it as a skipped key with a Warning
  Event.

Administrators extend both lists to the custom keys their rules write. Both
name the ServiceAccount `nodeecho` in `nodeecho-system`; installing NodeEcho
elsewhere means editing them.

## Compatibility

- This page is contract v1. Within v1, changes are additive only: new
  optional rule fields, with defaults that keep the current output.
- A change to an existing key's name, value format or presence rule is
  breaking. It uses a new key name or a new configuration `version`, and the
  previous behavior stays available for at least one release.

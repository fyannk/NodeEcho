# Contract v1

Status: **draft**, 2026-10-16. No implementation exists yet. This page lists
what a consumer can observe on its pods and what it may rely on; the
[design notes](design.md) keep the reasoning.

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

Pods remain in scope until they are deleted, whatever their phase. When a pod
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
  `topology.kubernetes.io/region` are left to Kubernetes unless the rule sets
  `labels.forceKubernetesManaged: true`. A selected pod missing such a key
  while its Node has it gets a Warning Event. `kubernetes.io/hostname` and
  every other key are copied whenever configured.

## Ownership

- NodeEcho writes with server-side apply, field manager `nodeecho/<rule name>`.
  It owns only the keys it wrote.
- It never overwrites a key owned by another writer, including a value set in
  the pod's template: the key is skipped, with a Warning Event on the pod and
  a metric.
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
```

A rule is refused, with a log line and a metric, while the other rules keep
running, when:

- `name` is missing, duplicated or not a DNS-1123 label, or `podSelector` is
  missing or empty;
- it has no `unschedulable`, `taints`, `labels` or `annotations` block, or a
  block's `copy` list is empty or repeats a key;
- a selector or a destination key is invalid;
- two of its keys resolve to the same pod key;
- its `unschedulable.key` or `taints.key` is also used by another rule.

An unknown `version` refuses the whole configuration and NodeEcho keeps the
last valid one.

## Protection

Pod `patch` rights cannot be limited to some keys, so anyone able to edit a
pod could forge an echoed value. NodeEcho ships an optional
`ValidatingAdmissionPolicy` that rejects changes to `nodeecho.fyannk.dev/*`
labels and annotations from any identity other than NodeEcho's
ServiceAccount. Administrators extend it to the custom keys their rules write.

## Compatibility

- This page is contract v1. Within v1, changes are additive only: new
  optional rule fields, with defaults that keep the current output.
- A change to an existing key's name, value format or presence rule is
  breaking. It uses a new key name or a new configuration `version`, and the
  previous behavior stays available for at least one release.

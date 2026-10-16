---
sidebar_position: 3
title: Admission policies
---

# Admission policies

RBAC cannot limit a pod `patch` right to some keys. NodeEcho ships two
optional `ValidatingAdmissionPolicy` objects in `config/policy`, one for each
direction:

| Policy | Applies to | Prevents |
| --- | --- | --- |
| `nodeecho-reserved-keys` | everyone but NodeEcho | forging, changing or removing a `nodeecho.fyannk.dev/` key |
| `nodeecho-writer-scope` | NodeEcho only | NodeEcho changing anything but its allowed keys |

Install both, after listing your rules' keys in them:

```bash
kubectl apply -k config/policy
```

## Reserved keys

Anyone who can edit a pod could otherwise forge `unschedulable=true` and
trigger a consumer's switchover. The policy rejects creating, updating or
removing `nodeecho.fyannk.dev/` labels and annotations, from any identity
other than NodeEcho's ServiceAccount, including in a pod template.

To reserve custom keys your rules write, extend its `prefixes` variable:

```yaml
- name: prefixes
  expression: "['nodeecho.fyannk.dev/', 'rack.example.org/']"
```

## NodeEcho's own scope

NodeEcho's ClusterRole would let it rewrite any pod: images, commands, or the
labels Services and NetworkPolicies select on. The policy limits its
ServiceAccount to:

- `UPDATE` on pods, no other operation and no subresource;
- no change to the spec, owner references or finalizers;
- changes only to the labels and annotations on its `allowed` list: exact
  keys, or prefixes ending in `/`.

The list must cover every destination key of every rule:

```yaml
- name: allowed
  expression: "['nodeecho.fyannk.dev/', 'topology.kubernetes.io/zone', 'rack.example.org/']"
```

A write outside it is rejected for the whole rule on that pod, and reported
as a `KeySkipped` Warning Event: `rule <name>: write rejected: ... NodeEcho
may not change <key>`.

## Blast radius

Both policies use `failurePolicy: Fail`.

- `nodeecho-reserved-keys` sees every pod creation and update in the
  cluster, but only reads labels and annotations. The end-to-end test checks
  it on a pod with 20 containers, 100 environment variables each and 1,000
  annotations, and through a rolling update of unrelated pods.
- `nodeecho-writer-scope` only matches NodeEcho's ServiceAccount: a mistake in
  it can stop NodeEcho, not other workloads.

Both name the ServiceAccount `system:serviceaccount:nodeecho-system:nodeecho`.
Install NodeEcho elsewhere and both must be edited: otherwise the reserved
keys policy also rejects NodeEcho, and the scope policy no longer applies.

## Check them

```bash
kubectl get validatingadmissionpolicy nodeecho-reserved-keys nodeecho-writer-scope \
  -o jsonpath='{range .items[*]}{.metadata.name}: {.status.typeChecking}{"\n"}{end}'
```

An empty `typeChecking` result means no warning.

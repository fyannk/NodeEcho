---
sidebar_position: 2
title: Metrics and Events
---

# Metrics and Events

## Metrics

Served on `:8080/metrics` (Prometheus text format), next to the standard
controller-runtime and Go runtime metrics.

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `nodeecho_config_valid` | gauge | | `1` when the last read configuration was valid, `0` when NodeEcho keeps an earlier one |
| `nodeecho_rules_active` | gauge | | rules applied, including refused rules kept at their last valid version |
| `nodeecho_rules_refused` | gauge | | rules refused in the last read configuration |
| `nodeecho_keys_skipped_total` | counter | `rule`, `reason` | configured keys not written on a pod |
| `nodeecho_pod_writes_total` | counter | `rule`, `result` | server-side apply requests sent to pods |

`reason` is one of:

- `owned-by-other`: another writer holds the key with another value;
- `invalid-value`: not a valid label value;
- `rejected`: the API server refused the write.

`result` is one of:

- `ok`;
- `conflict`: retried without the conflicting keys;
- `gone`: the pod was deleted or replaced meanwhile;
- `rejected`;
- `error`: retried with backoff.

The `rule` label is `(removed)` for the cleanup of a rule that no longer
exists.

## Events

Warning Events on the pod, from `nodeecho`, through the `events.k8s.io` API:

| Reason | Message |
| --- | --- |
| `KeySkipped` | `rule <name>: key <key> not written: <why>`, or `rule <name>: write rejected: <API error>` |
| `KubernetesTopologyMissing` | `rule <name>: label <key> is on node <node> but not on the pod; Kubernetes did not copy it, set labels.forceKubernetesManaged to copy it` |

## Probes

On `:8081`:

- `/healthz`: the process is alive.
- `/readyz`: on the elected instance, a valid configuration is loaded; a
  standby instance is always ready.

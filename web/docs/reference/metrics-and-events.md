---
sidebar_position: 2
title: Metrics and Events
---

# Metrics and Events

## Metrics

Served on `:8443/metrics` over HTTPS, to clients allowed to `GET /metrics`
(see [Observability](../operator/observability.md#metrics)), next to the
standard controller-runtime, client-go and Go runtime metrics.

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `nodeecho_build_info` | gauge | `version`, `go_version` | always `1` |
| `nodeecho_config_loads_total` | counter | `result` | reads of the rules ConfigMap: `valid`, `invalid` or `missing` |
| `nodeecho_config_last_valid_timestamp_seconds` | gauge | | time of the last valid read |
| `nodeecho_config_valid` | gauge | | `1` when the last read configuration was valid, `0` when NodeEcho keeps an earlier one |
| `nodeecho_rules_active` | gauge | | rules applied, including refused rules kept at their last valid version |
| `nodeecho_rules_refused` | gauge | | rules refused in the last read configuration |
| `nodeecho_keys_skipped_total` | counter | `rule`, `reason` | configured keys not written on a pod |
| `nodeecho_pod_writes_total` | counter | `rule`, `result` | server-side apply requests sent to pods |
| `nodeecho_evictions_total` | counter | `rule`, `response` | eviction requests for pods a rule with an `eviction` block selects; `response` is `allow` or `deny` |

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

Useful standard metrics: `controller_runtime_reconcile_errors_total` and
`controller_runtime_reconcile_time_seconds` (label `controller`: `pods`,
`rules`), `workqueue_queue_duration_seconds` and `workqueue_depth` (label
`name`), `leader_election_master_status`, `rest_client_requests_total`,
`process_resident_memory_bytes`.

## Events

Warning Events on the pod, from `nodeecho`, through the `events.k8s.io` API:

| Reason | Message |
| --- | --- |
| `KeySkipped` | `rule <name>: key <key> not written: <why>`, or `rule <name>: write rejected: <API error>` |
| `EvictionRequested` (Normal) | `rule <name>: eviction request received` |
| `EvictionRefused` | `rule <name>: eviction request refused: <message>` |

## Probes

On `:8081`:

- `/healthz`: the process is alive.
- `/readyz`: with the eviction webhook, every instance once it serves it;
  besides, the elected instance once a valid configuration is loaded. A
  standby instance does not wait for a configuration.

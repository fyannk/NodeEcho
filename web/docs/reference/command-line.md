---
sidebar_position: 3
title: Command line
---

# Command line

| Flag | Default | Meaning |
| --- | --- | --- |
| `--config-name` | `nodeecho` | name of the rules ConfigMap |
| `--config-namespace` | `$POD_NAMESPACE` | its namespace, NodeEcho's own; required |
| `--metrics-bind-address` | `:8080` | metrics endpoint; `0` disables it |
| `--health-probe-bind-address` | `:8081` | `/healthz` and `/readyz` |
| `--leader-elect` | `false` | leader election, for more than one replica; on in the shipped Deployment |
| `--zap-log-level`, `--zap-devel`, … | | logging, from controller-runtime's zap options |

Leader election uses the Lease `nodeecho.fyannk.dev` in NodeEcho's namespace,
and releases it on shutdown.

## Environment

| Variable | Meaning |
| --- | --- |
| `POD_NAMESPACE` | default for `--config-namespace`, set from the downward API |
| `GOMEMLIMIT` | Go runtime soft memory limit, see [Sizing](../operator/sizing.md) |
| `KUBECONFIG` | out of a cluster, the kubeconfig to use |

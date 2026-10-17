---
sidebar_position: 3
title: Command line
---

# Command line

| Flag | Default | Meaning |
| --- | --- | --- |
| `--config-name` | `nodeecho` | name of the rules ConfigMap |
| `--config-namespace` | `$POD_NAMESPACE` | its namespace, NodeEcho's own; required |
| `--metrics-bind-address` | `:8443`, or `:8080` without `--metrics-secure` | metrics endpoint; `0` disables it |
| `--metrics-secure` | `true` | HTTPS, with authentication and authorization through the API server |
| `--metrics-cert-dir` | | `tls.crt` and `tls.key` for the metrics endpoint; self-signed otherwise |
| `--health-probe-bind-address` | `:8081` | `/healthz` and `/readyz` |
| `--pprof-bind-address` | | pprof endpoint, for instance `127.0.0.1:6060`; off when empty |
| `--webhook-port` | `0` | port of the [eviction webhook](../operator/eviction-webhook.md); `0` disables it |
| `--cert-dir` | `/tmp/k8s-webhook-server/serving-certs` | the webhook's `tls.crt` and `tls.key`, mounted from a Secret or provided by OLM |
| `--cert-rotation` | `false` | generate and rotate the webhook certificate in `--cert-secret`, and inject its CA into `--webhook-config` |
| `--cert-secret` | `nodeecho-webhook-cert` | Secret of the rotated certificate |
| `--webhook-service` | `nodeecho-webhook` | Service of the webhook, named in its certificate |
| `--webhook-config` | `nodeecho-eviction` | ValidatingWebhookConfiguration receiving the CA |
| `--leader-elect` | `false` | leader election, for more than one replica; on in the shipped Deployment |
| `--version` | | print the version and exit |
| `--zap-log-level` | `info` | `info`, `error`, or a number for more detail (`1`, `2`) |
| `--zap-encoder` | `json` | `json` or `console` |
| `--zap-stacktrace-level` | `dpanic` | level from which stack traces are logged: only on panics by default |
| `--zap-time-encoding` | `iso8601` | time format |

Leader election uses the Lease `nodeecho.fyannk.dev` in NodeEcho's namespace,
and releases it on shutdown.

## Environment

| Variable | Meaning |
| --- | --- |
| `POD_NAMESPACE` | default for `--config-namespace`, set from the downward API |
| `GOMEMLIMIT` | Go runtime soft memory limit, see [Sizing](../operator/sizing.md) |
| `KUBECONFIG` | out of a cluster, the kubeconfig to use |

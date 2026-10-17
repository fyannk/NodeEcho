---
sidebar_position: 6
title: Observability
---

# Observability

NodeEcho exposes Prometheus metrics over HTTPS, writes structured JSON logs,
emits Events on the pods it skips, and can serve pprof. `config/prometheus`
and `config/grafana` hold a ServiceMonitor, alerts and a dashboard.

## Metrics

Two modes, chosen at install:

| Mode | Port | Who may scrape |
| --- | --- | --- |
| secure (default) | `https-metrics`, 8443 | clients the API server authenticates and authorizes for `GET /metrics` |
| plain | `http-metrics`, 8080 | anyone who reaches the pod |

Plain HTTP is for a scraper that sends no token, such as a classic
`kubernetes-pods` job. Choose it with `metrics.secure: false` in Helm, or the
`config/components/metrics-http` component; the container port, the Service
and the discovery annotations follow.

In secure mode, the endpoint lets in a ServiceAccount token bound to the
`nodeecho-metrics-reader` ClusterRole.

```bash
kubectl create clusterrolebinding prometheus-reads-nodeecho \
  --clusterrole=nodeecho-metrics-reader --serviceaccount=monitoring:prometheus-k8s
```

The certificate is self-signed unless `--metrics-cert-dir` points at a
directory holding `tls.crt` and `tls.key`, for instance a cert-manager
Secret mounted in the pod (`metrics.certSecret` in Helm).

Every metric is listed in [Metrics and Events](../reference/metrics-and-events.md).

### With the Prometheus Operator

```bash
kubectl apply -k config/prometheus
```

installs:

- a ServiceMonitor on the `nodeecho-metrics` Service, with Prometheus's own
  token;
- a PrometheusRule with the alerts below;
- the reader binding for kube-prometheus's `monitoring/prometheus-k8s`. Change
  it for another Prometheus.

The alerts aggregate gauges with `max()`: a standby replica runs no controller
and reports zeros. The Helm chart has the same with
`metrics.serviceMonitor.enabled`, `metrics.prometheusRule.enabled` and
`metrics.readers`.

### Without the Prometheus Operator

NodeEcho's pod can carry the annotations monitoring agents discover their
targets by, after the conventions of the
[CloudNativePG discovery annotations](https://github.com/fyannk/cloudnative-pg/pull/30).
They are off by default; in Helm, `metrics.discovery` lists the families:

| Family | Annotations | Read by |
| --- | --- | --- |
| `prometheus` | `prometheus.io/scrape`, `port`, `path`, `scheme` | a classic `kubernetes-pods` scrape job |
| `opentelemetry` | the `io.opentelemetry.discovery.*` hints of the metrics port and of the `manager` container's logs | an OpenTelemetry Collector's `receiver_creator` with the `k8s_observer` |

- Port and scheme follow the metrics mode. A classic Prometheus job sends no
  token, so pair `prometheus` with `metrics.secure: false`.
- With secure metrics, the OpenTelemetry hint scrapes over HTTPS, skips the
  certificate check (it does not cover the pod IP), and sends the collector's
  ServiceAccount token: bind the collector's ServiceAccount with
  `metrics.readers`.
- The logs hint parses the container log, then the JSON of each line.
- The annotations are on the pod only, never on a Service, so nothing is
  scraped twice.

```yaml
metrics:
  secure: false
  discovery: [prometheus, opentelemetry]
```

With kustomize, add the same annotations to the Deployment's pod template;
`helm template` with these values prints them.

## Alerts

All alerts select `job="nodeecho-metrics"`, the Service name; adapt it if you
scrape NodeEcho otherwise. They are unit-tested with promtool
(`make test-alerts`).

### NodeEchoDown

**Critical.** No NodeEcho instance has been scraped for 10 minutes.

Pods keep the values they have, but a new cordon or taint is not echoed, and
consumers lose that signal. Check the Deployment and its pods, then the
scrape: Service endpoints, the reader binding, the ServiceMonitor's labels.

### NodeEchoNoLeader

**Critical.** NodeEcho runs, but no instance has held the Lease
`nodeecho.fyannk.dev` for 5 minutes, so nothing is echoed.

Check the Lease (`kubectl -n nodeecho-system get lease nodeecho.fyannk.dev
-o yaml`), the Role granting `leases`, and the logs of the `leaderelection`
logger.

### NodeEchoConfigInvalid

**Warning.** The rules ConfigMap has been invalid or missing for 10 minutes.

NodeEcho keeps applying the last valid configuration, or writes nothing if it
never loaded one; rule changes do not apply. The log line `Invalid
configuration, keeping the last valid one` gives the parser's error. See
[Configuration](configuration.md#what-is-refused).

### NodeEchoRulesRefused

**Warning.** One or more rules have been refused for 10 minutes.

A refused rule is not applied; one that was already running keeps its last
valid version. The log line `Rule refused` gives the rule's index, name and
error.

### NodeEchoReconcileErrors

**Warning.** Reconciles have kept failing for 15 minutes.

The failed pods are retried with backoff. The log line `Reconciler error`
gives the error; a write the API server refuses is not an error, see the next
alert.

### NodeEchoWritesRejected

**Warning.** The API server rejected writes of a rule in the last 30 minutes.

Usually a destination key missing from the `nodeecho-writer-scope` policy's
allowed list, or a pod over the annotations size limit. The `KeySkipped`
Events on the pods, and the log line `Write rejected by the API server`,
give the API server's message. See
[Admission policies](admission-policies.md#nodeechos-own-scope).

### NodeEchoEchoSlow

**Warning.** For 15 minutes, the 99th percentile of the time a pod waits in
NodeEcho's queue has been above 60 seconds: Node changes reach pods late.

Look at the queue depth and the API requests on the dashboard. A rule change
on a very large cluster can do this briefly; see [Sizing](sizing.md).

### NodeEchoKeysSkipped

**Info.** A rule skipped keys in the last hour, because another writer holds
them (`owned-by-other`) or because a Node value is not a valid label value
(`invalid-value`). See [Troubleshooting](troubleshooting.md#events).

## Dashboard

`config/grafana/nodeecho.json` is a Grafana dashboard: configuration state,
active and refused rules, the leader, the version; pod writes, skipped keys,
echo latency, reconcile duration, queue depth; memory, CPU and API requests.

```bash
kubectl apply -k config/grafana
```

creates it as a ConfigMap labelled `grafana_dashboard: "1"`, which Grafana's
dashboard sidecar (kube-prometheus-stack, the Grafana chart) loads. It can
also be imported by hand. It picks its Prometheus data source from a
variable.

## Logs

One JSON object per line, on standard error:

```json
{"level":"info","ts":"2026-10-16T16:32:35.122Z","msg":"Write rejected by the API server","controller":"pods","Pod":{"name":"db-2","namespace":"e2e"},"namespace":"e2e","name":"db-2","reconcileID":"918a6ca9-…","rule":"outside","error":"pods \"db-2\" is forbidden: …"}
```

- `ts` is ISO 8601, in UTC; `level` is `info`, `error`, or a number for
  debug levels; `msg` is constant for a given event, so it can be searched
  for.
- Everything variable is a field: `rule`, `key`, `reason`, `configMap`, and
  the pod or ConfigMap being reconciled with its `reconcileID`.
- client-go's own logs and Go's standard library logs go through the same
  logger, so every line has this format. The end-to-end test checks it.

| Level | Flag | Logged |
| --- | --- | --- |
| 0 | default | start, configuration loads and refusals, rejected writes, errors |
| 1 | `--zap-log-level=1` | each pod write, each skipped key |
| 2+ | `--zap-log-level=2` | controller-runtime's own detail |

`--zap-encoder=console` switches to human-readable lines, for a terminal.

## pprof

Off by default. `--pprof-bind-address=127.0.0.1:6060` serves Go's profiles on
the pod's loopback only, reachable with a port-forward:

```bash
kubectl -n nodeecho-system port-forward deploy/nodeecho 6060
go tool pprof http://127.0.0.1:6060/debug/pprof/heap
```

The endpoint has no authentication: bind it to `127.0.0.1`, never to `:6060`
on a shared network.

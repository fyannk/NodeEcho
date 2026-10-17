{{/*
Expand the name of the chart.
*/}}
{{- define "nodeecho.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "nodeecho.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Common labels.
*/}}
{{- define "nodeecho.labels" -}}
app.kubernetes.io/name: {{ include "nodeecho.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | quote }}
{{- end -}}

{{/*
Selector labels.
*/}}
{{- define "nodeecho.selectorLabels" -}}
app.kubernetes.io/name: {{ include "nodeecho.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
The image. An empty tag means the chart's appVersion.
*/}}
{{- define "nodeecho.image" -}}
{{- printf "%s:%s" .Values.image.repository (.Values.image.tag | default .Chart.AppVersion) -}}
{{- end -}}

{{/*
The identity NodeEcho runs as, named by the admission policies.
*/}}
{{- define "nodeecho.user" -}}
{{- printf "system:serviceaccount:%s:%s" .Release.Namespace (include "nodeecho.fullname" .) -}}
{{- end -}}

{{/*
Metrics: port, scheme and port name follow metrics.secure.
*/}}
{{- define "nodeecho.metricsPort" -}}
{{- if .Values.metrics.port -}}{{ .Values.metrics.port }}{{- else if .Values.metrics.secure -}}8443{{- else -}}8080{{- end -}}
{{- end -}}
{{- define "nodeecho.metricsScheme" -}}
{{- if .Values.metrics.secure -}}https{{- else -}}http{{- end -}}
{{- end -}}
{{- define "nodeecho.metricsPortName" -}}
{{- if .Values.metrics.secure -}}https-metrics{{- else -}}http-metrics{{- end -}}
{{- end -}}

{{/*
The webhook Service, named in its certificate.
*/}}
{{- define "nodeecho.webhookService" -}}
{{- printf "%s-webhook" (include "nodeecho.fullname" .) -}}
{{- end -}}

{{/*
The pod keys the rules write, as a JSON list: the allowed list of the
writer-scope policy.
*/}}
{{- define "nodeecho.ruleKeys" -}}
{{- $keys := list -}}
{{- range .Values.rules -}}
{{- if hasKey . "unschedulable" -}}
{{- $keys = append $keys (default "nodeecho.fyannk.dev/unschedulable" (.unschedulable | default dict).key) -}}
{{- end -}}
{{- if hasKey . "taints" -}}
{{- $keys = append $keys (default "nodeecho.fyannk.dev/taints" .taints.key) -}}
{{- end -}}
{{- with .labels -}}
{{- $prefix := default "" .prefix -}}
{{- range .copy -}}{{- $keys = append $keys (printf "%s%s" $prefix .) -}}{{- end -}}
{{- end -}}
{{- with .annotations -}}
{{- $prefix := default "" .prefix -}}
{{- range .copy -}}{{- $keys = append $keys (printf "%s%s" $prefix .) -}}{{- end -}}
{{- end -}}
{{- if hasKey . "eviction" -}}
{{- $keys = append $keys (default "nodeecho.fyannk.dev/eviction-requested" (.eviction | default dict).key) -}}
{{- end -}}
{{- end -}}
{{- $keys | uniq | toJson -}}
{{- end -}}

{{/*
Discovery annotations, after the conventions of cloudnative-pg PR #30:
prometheus.io/* for a classic kubernetes-pods job, and the
io.opentelemetry.discovery.* hints of the collector's receiver_creator for
the metrics (prometheus receiver) and the JSON logs of the manager
container. With secure metrics, the hint sends the collector's
ServiceAccount token, which needs the metrics reader ClusterRole.
*/}}
{{- define "nodeecho.discoveryAnnotations" -}}
{{- $port := include "nodeecho.metricsPort" . -}}
{{- $scheme := include "nodeecho.metricsScheme" . -}}
{{- range .Values.metrics.discovery }}
{{- if eq . "prometheus" }}
prometheus.io/scrape: "true"
prometheus.io/port: {{ $port | quote }}
prometheus.io/path: /metrics
prometheus.io/scheme: {{ $scheme }}
{{- else if eq . "opentelemetry" }}
io.opentelemetry.discovery.metrics.{{ $port }}/enabled: "true"
io.opentelemetry.discovery.metrics.{{ $port }}/scraper: prometheus
io.opentelemetry.discovery.metrics.{{ $port }}/config: |
  config:
    scrape_configs:
      - job_name: nodeecho
        metrics_path: /metrics
        scheme: {{ $scheme }}
  {{- if eq $scheme "https" }}
        tls_config:
          insecure_skip_verify: true
        authorization:
          credentials_file: /var/run/secrets/kubernetes.io/serviceaccount/token
  {{- end }}
        static_configs:
          - targets: ["`endpoint`"]
io.opentelemetry.discovery.logs.manager/enabled: "true"
io.opentelemetry.discovery.logs.manager/config: |
  operators:
    - type: container
      id: container-parser
    - type: json_parser
{{- else }}
{{- fail (printf "metrics.discovery: unknown family %q, expected prometheus or opentelemetry" .) }}
{{- end }}
{{- end }}
{{- end -}}

---
sidebar_position: 2
title: Status
---

# Status

**Early implementation, not production-ready.**

## Done

- Contract v1, implemented in Go with controller-runtime.
- Unit tests, fuzz tests, and an envtest suite against a real API server
  covering conflicts, foreign keys, scope changes, rule renames, invalid
  configurations, rejected writes and replaced pods.
- An end-to-end test on throwaway k3d clusters for Kubernetes 1.34, 1.36,
  and 1.36 with `PodTopologyLabelsAdmission` off: echo and cleanup, both
  admission policies, restarts, leader failover, invalid and missing
  configurations, and a cordon reaching 120 pods.
- Authenticated HTTPS metrics, Prometheus alerts with unit tests, a Grafana
  dashboard, structured JSON logs and an optional pprof endpoint.
- A scale measurement on fake Nodes: about 120 MiB at 20,000 pods, one write
  per pod that needs one. See [Sizing](../operator/sizing.md).

## Missing before production

- **A release.** No image is published, and the manifests use
  `nodeecho:latest`. The Dockerfile builds `linux/amd64` only.
- **A soak test.** The tests run for minutes, not hours, and do not restart
  the API server or break watches.
- **A staging run with a consumer.** Upstream CloudNativePG does not read the
  keys; an integration is required, and other consumers are not validated.
- **Hardening.** There is no NetworkPolicy, PodDisruptionBudget or
  PriorityClass in the manifests, and the metrics certificate is self-signed
  unless you provide one.
- **Other platforms.** EKS, GKE, AKS and OpenShift are not tested, nor is an
  upgrade between NodeEcho versions.

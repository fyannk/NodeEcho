#!/usr/bin/env bash
# Copyright © contributors to the NodeEcho project.
# SPDX-License-Identifier: Apache-2.0
#
# Checks and unit-tests the Prometheus alerts in config/prometheus/alerts.yaml
# with promtool, in containers: the PrometheusRule's spec is extracted with yq
# into a plain rules file, then checked and tested against alerts_test.yaml.

set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
PROMETHEUS=prom/prometheus:v3.13.4@sha256:87861b8cf91579109319ebc300f3f1060e6da9c05d6ae8ad15a20c879e84e32e
YQ=mikefarah/yq:4.54.1@sha256:4b3d9475d65571d28cbb19544d3820ec2945e4c8b2f18279394282b8dc3a592e

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
chmod 755 "$WORK"

docker run --rm -i "$YQ" '.spec' <"$ROOT/config/prometheus/alerts.yaml" >"$WORK/rules.yaml"
cp "$ROOT/config/prometheus/alerts_test.yaml" "$WORK/"
chmod 644 "$WORK"/*.yaml

promtool() { docker run --rm -v "$WORK:/work:ro" -w /work --entrypoint promtool "$PROMETHEUS" "$@"; }
promtool check rules rules.yaml
promtool test rules alerts_test.yaml

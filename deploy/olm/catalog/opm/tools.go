/*
Copyright © contributors to the NodeEcho project.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

//go:build tools

// Package opm pins the two programs the OLM catalog image runs, opm and
// grpc_health_probe, so that catalog.Dockerfile builds them from source with
// the repository's Go toolchain.
package opm

import (
	_ "github.com/grpc-ecosystem/grpc-health-probe"
	_ "github.com/operator-framework/operator-registry/cmd/opm"
)

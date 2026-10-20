#!/bin/bash
# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      https://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Helpers shared by the cloudbuild test scripts. Source this file.

# wait_topology_ready NAMESPACE [TIMEOUT]
#
# Waits for every pod in the topology namespace to be Ready. `kne create` only
# logs a warning if node status times out, and it does not treat errors such as
# CreateContainerError as fatal, so it can exit 0 with pods that never start.
# On failure this dumps pod state and events and returns non-zero.
wait_topology_ready() {
	local ns="$1"
	local timeout="${2:-5m}"
	if kubectl wait --for=condition=Ready pod --all -n "$ns" --timeout="$timeout"; then
		kubectl get pods -n "$ns" -o wide
		return 0
	fi
	echo "ERROR: pods in namespace ${ns} did not become Ready within ${timeout}" >&2
	kubectl get pods -n "$ns" -o wide || true
	kubectl describe pods -n "$ns" || true
	kubectl get events -n "$ns" --sort-by=.lastTimestamp || true
	return 1
}

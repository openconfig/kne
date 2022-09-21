#!/bin/bash
# Copyright 2022 Google LLC
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

set -xe

# Sync the existing KNE checkout to the desired branch and commit
pushd /home/user/kne
git fetch remote $BRANCH_NAME
git merge $SHORT_SHA

# Rebuild the kne cli
pushd kne_cli
go build -o $(go env GOPATH)/bin/kne

# Deploy a cluster + topo
pushd /home/user
kne deploy kne-internal/deploy/kne/kind-bridge.yaml
kne create kne-internal/examples/multivendor/multivendor.pbtxt

# Log topology
kubectl get pods -A
kubectl get services -A

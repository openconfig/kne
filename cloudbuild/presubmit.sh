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

export PATH=${PATH}:/usr/local/go/bin
gopath=$(go env GOPATH)
export PATH=${PATH}:$gopath/bin

# Replace exisiting kne repo with new version
rm -r "$HOME/kne"
cp -r /tmp/workspace "$HOME/kne"

# Rebuild the kne cli
pushd "$HOME/kne/kne_cli"
go build -o "$gopath/bin/kne"
popd

# Deploy a cluster + topo
pushd "$HOME"
kne deploy kne/deploy/kne/kind-bridge.yaml

docker pull us-west1-docker.pkg.dev/gep-kne/arista/ceos:ga
docker tag us-west1-docker.pkg.dev/gep-kne/arista/ceos:ga ceos:latest
kind load docker-image --name=kne ceos:latest

docker pull us-west1-docker.pkg.dev/gep-kne/cisco/ios-xr/xrd:ga
docker tag us-west1-docker.pkg.dev/gep-kne/cisco/ios-xr/xrd:ga xrd:latest
kind load docker-image --name=kne xrd:latest

docker pull us-west1-docker.pkg.dev/gep-kne/juniper/cptx:ga
docker tag us-west1-docker.pkg.dev/gep-kne/juniper/cptx:ga cptx:latest
kind load docker-image --name=kne cptx:latest

docker pull us-west1-docker.pkg.dev/gep-kne/nokia/srlinux:ga
docker tag us-west1-docker.pkg.dev/gep-kne/nokia/srlinux:ga ghcr.io/nokia/srlinux:latest
kind load docker-image --name=kne ghcr.io/nokia/srlinux:latest

kne create kne/examples/multivendor/multivendor.pb.txt

# Run an ondatra test
cat >config.yaml << EOF
username: admin
password: admin
topology: ${HOME}/kne/examples/multivendor/multivendor.pb.txt
EOF

go test -v kne/cloudbuild/presubmit/presubmit_test.go -config config.yaml -testbed cloudbuild/presubmit/testbed.textproto

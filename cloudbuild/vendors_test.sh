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
go build -o kne
cli="$HOME/kne/kne_cli/kne"
popd

# Deploy a cluster + topo
pushd "$HOME"
$cli deploy kne/cloudbuild/vendors/deployment.yaml --report_usage=false
$cli create kne/cloudbuild/vendors/topology.textproto --report_usage=false
popd

# Arista cEOS is not fully healthy after topo creation
sleep 60

# Run an ondatra test
pushd "$HOME/kne/cloudbuild"
go test -v vendors/vendors_test.go \
  -testbed testbed.textproto \
  -topology topology.textproto \
  -vendor_creds ARISTA/admin/admin \
  -vendor_creds JUNIPER/root/Google123 \
  -vendor_creds CISCO/cisco/cisco123 \
  -vendor_creds NOKIA/admin/NokiaSrl1!
popd

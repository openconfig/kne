#!/bin/bash
# Copyright 2023 Google LLC
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

# Deploy a kind cluster
pushd "$HOME"
$cli deploy kne/deploy/kne/kind-bridge.yaml --report_usage=false

kubectl get pods -A

# Redeploy the same cluster
$cli deploy kne/deploy/kne/kind-bridge.yaml --report_usage=false

kubectl get pods -A

# Cleanup the cluster
$cli teardown kne/deploy/kne/kind-bridge.yaml

# Create a kind cluster with GAR access and image loading
cat >/tmp/dep-cfg.yaml << EOF
cluster:
  kind: Kind
  spec:
    name: kne
    recycle: True
    version: v0.17.0
    image: 'kindest/node:v1.26.0'
    googleArtifactRegistries:
      - us-west1-docker.pkg.dev
    containerImages:
      'us-west1-docker.pkg.dev/kne-external/kne/networkop/init-wait:ga': 'networkop/init-wait:latest'
    config: ${HOME}/kne/manifests/kind/config.yaml
    additionalManifests:
      - ${HOME}/kne/manifests/kind/bridge.yaml
ingress:
  kind: MetalLB
  spec:
    manifest: ${HOME}/kne/manifests/metallb/manifest.yaml
    ip_count: 100
cni:
  kind: Meshnet
  spec:
    manifest: ${HOME}/kne/manifests/meshnet/grpc/manifest.yaml
EOF

$cli deploy /tmp/dep-cfg.yaml --report_usage=false

kubectl get pods -A

# Check for existence of preloaded image
if ! docker exec kne-control-plane crictl images | grep "docker.io/networkop/init-wait"; then
  echo "Unable to find preloaded image in cluster"
  exit 1
fi

# Cleanup the cluster
$cli teardown kne/deploy/kne/kind-bridge.yaml

# Create a kubeadm cluster
$cli deploy kne/deploy/kne/kubeadm.yaml --report_usage=false

kubectl get pods -A

# Cleanup the kubeadm cluster
$cli teardown kne/deploy/kne/kubeadm.yaml

## Create a kubeadm single node cluster
sudo kubeadm init --cri-socket unix:///var/run/containerd/containerd.sock --pod-network-cidr 10.244.0.0/16
mkdir -p "$HOME"/.kube
sudo cp /etc/kubernetes/admin.conf "$HOME"/.kube/config
sudo chown "$(id -u)":"$(id -g)" "$HOME"/.kube/config
kubectl taint nodes --all node-role.kubernetes.io/control-plane-  # allows pods to be scheduled on control plane node
kubectl apply -f kne/manifests/flannel/manifest.yaml
docker network create multinode

# Deploy an external cluster
$cli deploy kne/deploy/kne/external-multinode.yaml --report_usage=false

kubectl get pods -A

# Create a simple lemming topology
$cli create kne/examples/openconfig/lemming.pb.txt --report_usage=false

kubectl get pods -A

kubectl get services -A

# Use the KNE cli to interact with the topology
$cli show kne/examples/openconfig/lemming.pb.txt

$cli topology service kne/examples/openconfig/lemming.pb.txt

# Delete the topology
$cli delete kne/examples/openconfig/lemming.pb.txt

# Cleanup the cluster (no-op with external cluster)
$cli teardown kne/deploy/kne/external-multinode.yaml

popd

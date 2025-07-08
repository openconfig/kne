packer {
  required_plugins {
    googlecompute = {
      version = "= 1.1.6"
      source  = "github.com/hashicorp/googlecompute"
    }
  }
}

variable "short_sha" {
  type = string
}

variable "branch_name" {
  type = string
}

variable "build_id" {
  type = string
}

variable "zone" {
  type    = string
  default = "us-central1-b"
}

source "googlecompute" "kne-image" {
  project_id          = "gep-kne"
  source_image_family = "debian-12"
  disk_size           = 50
  image_name          = "kne-external-${var.build_id}"
  image_family        = "kne-external-untested"
  image_labels = {
    "kne_gh_commit_sha" : "${var.short_sha}",
    "kne_gh_branch_name" : "${var.branch_name}",
    "cloud_build_id" : "${var.build_id}",
  }
  image_description     = "Debian based linux VM image with KNE and all external dependencies installed."
  ssh_username          = "user"
  machine_type          = "e2-medium"
  zone                  = "${var.zone}"
  service_account_email = "packer@gep-kne.iam.gserviceaccount.com"
  use_internal_ip       = true
  scopes                = ["https://www.googleapis.com/auth/cloud-platform"]
  state_timeout         = "15m"
}

build {
  name    = "kne-builder"
  sources = ["sources.googlecompute.kne-image"]

  provisioner "shell" {
    inline = [
      "echo Installing golang...",
      "curl -O https://dl.google.com/go/go1.24.2.linux-amd64.tar.gz",
      "sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.24.2.linux-amd64.tar.gz",
      "rm go1.24.2.linux-amd64.tar.gz",
      "echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc",
      "echo 'export PATH=$PATH:$(go env GOPATH)/bin' >> ~/.bashrc",
      "/usr/local/go/bin/go version",
    ]
  }

  provisioner "shell" {
    inline = [
      "echo Installing libpcap-dev...",
      "sudo apt-get update",
      "sudo apt-get install libpcap-dev -y",
    ]
  }

  provisioner "shell" {
    inline = [
      "echo Installing docker...",
      "sudo apt-get update",
      "sudo apt-get install apt-transport-https ca-certificates curl gnupg lsb-release -y",
      "curl -fsSL https://download.docker.com/linux/debian/gpg | sudo gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg",
      "echo \"deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/debian $(lsb_release -cs) stable\" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null",
      "sudo apt-get update",
      "sudo apt-get install docker-ce docker-ce-cli containerd.io build-essential -y",
      "sudo usermod -aG docker $USER",
      "sudo docker version",
      "sudo apt-get install openvswitch-switch-dpdk -y",   # install openvswitch for cisco containers
      "echo \"fs.inotify.max_user_instances=128000\" | sudo tee -a /etc/sysctl.conf", # configure inotify for cisco xrd containers
      "echo \"fs.inotify.max_user_watches=25600000\" | sudo tee -a /etc/sysctl.conf", # configure inotify for cisco xrd containers
      "echo \"fs.inotify.max_queued_events=13107200\" | sudo tee -a /etc/sysctl.conf", # configure inotify for cisco xrd containers
      "echo \"kernel.pid_max=1048575\" | sudo tee -a /etc/sysctl.conf",              # configure pid_max for cisco 8000e containers
      "echo \"br_netfilter\" | sudo tee -a /etc/modules-load.d/br_netfilter.conf",   # ensure br_netfilter module is loaded instead of relying on docker-ce (https://github.com/moby/moby/issues/48948)
      "sudo modprobe br_netfilter",
      "echo \"1\" > sudo tee /proc/sys/net/bridge/bridge-nf-call-iptables",
      "echo \"1\" > sudo tee /proc/sys/net/ipv4/ip_forward",
      "sudo sysctl --system",
      "sudo sysctl -p",
    ]
  }

  provisioner "shell" {
    inline = [
      "echo Installing kubectl...",
      "curl -fsSL https://pkgs.k8s.io/core:/stable:/v1.29/deb/Release.key | sudo gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg",
      "echo \"deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v1.29/deb/ /\" | sudo tee /etc/apt/sources.list.d/kubernetes.list",
      "sudo apt-get update",
      "sudo apt-get install kubelet kubeadm kubectl -y",
      "kubectl version --client",
      "echo 'source <(kubectl completion bash)' >> ~/.bashrc",
      "echo 'alias k=kubectl' >> ~/.bashrc",
      "echo 'complete -o default -F __start_kubectl k' >> ~/.bashrc",
    ]
  }

  provisioner "shell" {
    inline = [
      "echo Installing golang license tool...",
      "/usr/local/go/bin/go install github.com/google/go-licenses@latest",
      "curl --create-dirs -o third_party/licenses/go-licenses/LICENSE https://raw.githubusercontent.com/google/go-licenses/master/LICENSE",
    ]
  }

  provisioner "shell" {
    inline = [
      "echo Installing multinode cluster dependencies...",
      "git clone https://github.com/flannel-io/flannel.git",
      "curl --create-dirs -o third_party/licenses/flannel/LICENSE https://raw.githubusercontent.com/flannel-io/flannel/master/LICENSE",
      "sudo mkdir -p /etc/containerd",
      "sudo containerd config default | sudo tee /etc/containerd/config.toml",
      "sudo sed -i 's/^disabled_plugins = \\[\"cri\"\\]/#disabled_plugins = \\[\"cri\"\\]/' /etc/containerd/config.toml",
      "echo \"Setting containerd to use systemd cgroup driver...\"",
      "sudo sed -i 's/SystemdCgroup = false/SystemdCgroup = true/' /etc/containerd/config.toml",
      "sudo systemctl restart containerd",
      "cd $HOME",
      "git clone https://github.com/kubernetes/cloud-provider-gcp.git",
      "curl -Lo bazel https://github.com/bazelbuild/bazelisk/releases/download/v1.19.0/bazelisk-linux-amd64 && sudo install bazel /usr/local/bin/",
      "cd cloud-provider-gcp",
      "curl --create-dirs -o third_party/licenses/cloud-provider-gcp/LICENSE https://raw.githubusercontent.com/kubernetes/cloud-provider-gcp/master/LICENSE",
      "bazel build cmd/auth-provider-gcp",
      "sudo mkdir -p /etc/kubernetes/bin/",
      "sudo cp bazel-bin/cmd/auth-provider-gcp/auth-provider-gcp_/auth-provider-gcp /etc/kubernetes/bin/",
    ]
  }

  provisioner "shell" {
    inline = [
      "echo Installing kind...",
      "/usr/local/go/bin/go install sigs.k8s.io/kind@v0.24.0",
      "curl --create-dirs -o third_party/licenses/kind/LICENSE https://raw.githubusercontent.com/kubernetes-sigs/kind/main/LICENSE",
      "sudo cp /home/$USER/go/bin/kind /usr/local/bin/",
      "/home/$USER/go/bin/kind version",
    ]
  }

  provisioner "shell" {
    inline = [
      "echo Cloning openconfig/kne github repo...",
      "sudo apt-get install git -y",
      "git clone -b ${var.branch_name} https://github.com/openconfig/kne.git",
      "cd kne",
      "PATH=$PATH:/usr/local/go/bin",
      "/home/$USER/go/bin/go-licenses check github.com/openconfig/kne/kne_cli --ignore=$(go list std | awk 'NR > 1 { printf(\",\") } { printf(\"%s\",$0) } END { print \"\" }')",
      "/home/$USER/go/bin/go-licenses save github.com/openconfig/kne/kne_cli --save_path=\"../third_party/licenses/kne_cli\" --ignore=$(go list std | awk 'NR > 1 { printf(\",\") } { printf(\"%s\",$0) } END { print \"\" }')",
      "cd kne_cli",
      "/usr/local/go/bin/go build -o kne",
      "sudo cp kne /usr/local/bin/",
      "cd ../controller/server",
      "/usr/local/go/bin/go build",
    ]
  }

  provisioner "shell" {
    inline = [
      "echo Cloning openconfig/ondatra github repo...",
      "git clone https://github.com/openconfig/ondatra.git",
      "cd ondatra",
      "PATH=$PATH:/usr/local/go/bin",
      "/home/$USER/go/bin/go-licenses check github.com/openconfig/ondatra --ignore=$(go list std | awk 'NR > 1 { printf(\",\") } { printf(\"%s\",$0) } END { print \"\" }')",
      "/home/$USER/go/bin/go-licenses save github.com/openconfig/ondatra --save_path=\"../third_party/licenses/ondatra\" --ignore=$(go list std | awk 'NR > 1 { printf(\",\") } { printf(\"%s\",$0) } END { print \"\" }')",
    ]
  }

  provisioner "shell" {
    inline = [
      "echo Installing openconfig tools...",
      "sudo apt-get install tree -y",
      "bash -c \"$(curl -sL https://get-gnmic.openconfig.net)\"",
      "bash -c \"$(curl -sL https://get-gribic.kmrd.dev)\"",
      "bash -c \"$(curl -sL https://get-gnoic.kmrd.dev)\"",
    ]
  }
}

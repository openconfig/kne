packer {
  required_plugins {
    googlecompute = {
      version = ">= 1.1.1"
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
      "curl -O https://dl.google.com/go/go1.21.3.linux-amd64.tar.gz",
      "sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.21.3.linux-amd64.tar.gz",
      "rm go1.21.3.linux-amd64.tar.gz",
      "echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc",
      "echo 'export PATH=$PATH:$(go env GOPATH)/bin' >> ~/.bashrc",
      "/usr/local/go/bin/go version",
    ]
  }

  provisioner "shell" {
    inline = [
      "echo Installing docker...",
      "sudo apt-get -o DPkg::Lock::Timeout=60 update",
      "sudo apt-get -o DPkg::Lock::Timeout=60 install apt-transport-https ca-certificates curl gnupg lsb-release -y",
      "curl -fsSL https://download.docker.com/linux/debian/gpg | sudo gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg",
      "echo \"deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/debian $(lsb_release -cs) stable\" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null",
      "sudo apt-get -o DPkg::Lock::Timeout=60 update",
      "sudo apt-get -o DPkg::Lock::Timeout=60 install docker-ce docker-ce-cli containerd.io build-essential -y",
      "sudo usermod -aG docker $USER",
      "sudo docker version",
      "sudo apt-get -o DPkg::Lock::Timeout=60 install openvswitch-switch-dpdk -y",   # install openvswitch for cisco containers
      "echo \"fs.inotify.max_user_instances=64000\" | sudo tee -a /etc/sysctl.conf", # configure inotify for cisco xrd containers
      "echo \"kernel.pid_max=1048575\" | sudo tee -a /etc/sysctl.conf",              # configure pid_max for cisco 8000e containers
      "sudo sysctl -p",
    ]
  }

  provisioner "shell" {
    inline = [
      "echo Installing kubectl...",
      "curl -fsSL https://packages.cloud.google.com/apt/doc/apt-key.gpg | sudo gpg --dearmor -o /etc/apt/keyrings/kubernetes-archive-keyring.gpg",
      "echo \"deb [signed-by=/etc/apt/keyrings/kubernetes-archive-keyring.gpg] https://apt.kubernetes.io/ kubernetes-xenial main\" | sudo tee /etc/apt/sources.list.d/kubernetes.list",
      "sudo apt-get -o DPkg::Lock::Timeout=60 update",
      "sudo apt-get -o DPkg::Lock::Timeout=60 install kubelet kubeadm kubectl -y",
      "kubectl version --client",
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
      "git clone https://github.com/Mirantis/cri-dockerd.git --branch v0.3.1",
      "cd cri-dockerd",
      "PATH=$PATH:/usr/local/go/bin",
      "/home/$USER/go/bin/go-licenses check github.com/Mirantis/cri-dockerd",
      "/home/$USER/go/bin/go-licenses save github.com/Mirantis/cri-dockerd --save_path=\"../third_party/licenses/cri-dockerd\"",
      "/usr/local/go/bin/go build",
      "sudo cp cri-dockerd /usr/local/bin/",
      "sudo cp -a packaging/systemd/* /etc/systemd/system",
      "sudo sed -i -e 's,/usr/bin/cri-dockerd,/usr/local/bin/cri-dockerd,' /etc/systemd/system/cri-docker.service",
      "sudo systemctl enable cri-docker.socket",
    ]
  }

  provisioner "shell" {
    inline = [
      "echo Installing kind...",
      "/usr/local/go/bin/go install sigs.k8s.io/kind@v0.19.0",
      "curl --create-dirs -o third_party/licenses/kind/LICENSE https://raw.githubusercontent.com/kubernetes-sigs/kind/main/LICENSE",
      "sudo cp /home/$USER/go/bin/kind /usr/local/bin/",
      "/home/$USER/go/bin/kind version",
    ]
  }

  provisioner "shell" {
    inline = [
      "echo Cloning openconfig/kne github repo...",
      "sudo apt-get -o DPkg::Lock::Timeout=60 install git -y",
      "git clone -b ${var.branch_name} https://github.com/openconfig/kne.git",
      "cd kne",
      "PATH=$PATH:/usr/local/go/bin",
      "/home/$USER/go/bin/go-licenses check github.com/openconfig/kne/kne_cli",
      "/home/$USER/go/bin/go-licenses save github.com/openconfig/kne/kne_cli --save_path=\"../third_party/licenses/kne_cli\"",
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
      "/home/$USER/go/bin/go-licenses check github.com/openconfig/ondatra",
      "/home/$USER/go/bin/go-licenses save github.com/openconfig/ondatra --save_path=\"../third_party/licenses/ondatra\"",
    ]
  }
}

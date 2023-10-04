packer {
  required_plugins {
    googlecompute = {
      version = ">= 1.1.1"
      source = "github.com/hashicorp/googlecompute"
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
  source_image_family = "ubuntu-2204-lts"
  disk_size           = 50
  image_name          = "kne-ubuntu22-${var.build_id}"
  image_family        = "kne-ubuntu22-untested"
  image_labels = {
    "kne_gh_commit_sha" : "${var.short_sha}",
    "kne_gh_branch_name" : "${var.branch_name}",
    "cloud_build_id" : "${var.build_id}",
  }
  image_description     = "Ubuntu based linux VM image with KNE and all internal dependencies installed."
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
      "echo Waiting for initial updates...",
      "/usr/bin/cloud-init status --wait",
      "sleep 60",
    ]
  }

  provisioner "shell" {
    inline = [
      "echo Installing golang...",
      "curl -O https://dl.google.com/go/go1.20.1.linux-amd64.tar.gz",
      "sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.20.1.linux-amd64.tar.gz",
      "rm go1.20.1.linux-amd64.tar.gz",
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
      "curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg",
      "echo \"deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable\" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null",
      "sudo apt-get -o DPkg::Lock::Timeout=60 update",
      "sudo apt-get -o DPkg::Lock::Timeout=60 install docker-ce docker-ce-cli containerd.io build-essential -y",
      "sudo usermod -aG docker $USER",
      "sudo docker version",
      "sudo apt-get -o DPkg::Lock::Timeout=60 install openvswitch-switch-dpdk -y",   # install openvswitch for cisco containers
      "echo \"fs.inotify.max_user_instances=64000\" | sudo tee -a /etc/sysctl.conf", # configure inotify for cisco containers
      "echo \"kernel.pid_max=1048575\" | sudo tee -a /etc/sysctl.conf",              # configure pid_max for cisco containers
      "sudo sysctl -p",
      "echo Pulling containers...",
      "gcloud auth configure-docker us-west1-docker.pkg.dev -q",      # configure sudoless docker
      "sudo gcloud auth configure-docker us-west1-docker.pkg.dev -q", # configure docker with sudo
      "sudo docker pull us-west1-docker.pkg.dev/gep-kne/arista/ceos:ga",
      "sudo docker pull us-west1-docker.pkg.dev/gep-kne/cisco/xrd:ga",
      "sudo docker pull us-west1-docker.pkg.dev/gep-kne/cisco/8000e:ga",
      "sudo docker pull us-west1-docker.pkg.dev/gep-kne/juniper/cptx:ga",
      "sudo docker pull us-west1-docker.pkg.dev/gep-kne/nokia/srlinux:ga",
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
      "echo Installing multinode cluster dependencies...",
      "git clone https://github.com/flannel-io/flannel.git",
      "git clone https://github.com/Mirantis/cri-dockerd.git --branch v0.3.1",
      "cd cri-dockerd",
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
      "sudo cp /home/$USER/go/bin/kind /usr/local/bin/",
      "/home/$USER/go/bin/kind version",
    ]
  }

  provisioner "shell" {
    inline = [
      "echo Cloning openconfig/kne github repo...",
      "sudo apt-get -o DPkg::Lock::Timeout=60 install git -y",
      "git clone -b ${var.branch_name} https://github.com/openconfig/kne.git",
      "cd kne/kne_cli",
      "/usr/local/go/bin/go build -o kne",
      "sudo cp kne /usr/local/bin/",
      "cd ../controller/server",
      "/usr/local/go/bin/go build",
      "cd $HOME",
      "mkdir -p .config/kne",
      "echo \"report_usage: true\" > .config/kne/config.yaml",
    ]
  }

  provisioner "shell" {
    inline = [
      "echo Cloning openconfig/ondatra github repo...",
      "git clone https://github.com/openconfig/ondatra.git",
    ]
  }

  provisioner "shell" {
    inline = [
      "echo Cloning internal cloud source repos...",
      "gcloud source repos clone kne-internal --project=gep-kne",
      "cd kne-internal",
      "/usr/local/go/bin/go get -d ./...",
      "cd proxy/server",
      "/usr/local/go/bin/go build",
      "cd ../../kneproxy",
      "/usr/local/go/bin/go build",
    ]
  }

  provisioner "shell" {
    inline = [
      "echo Installing Google cloud ops agent...",
      "curl -sSO https://dl.google.com/cloudagents/add-google-cloud-ops-agent-repo.sh",
      "sudo bash add-google-cloud-ops-agent-repo.sh --also-install",
      "rm add-google-cloud-ops-agent-repo.sh",
    ]
  }
}

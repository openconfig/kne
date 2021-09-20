source "googlecompute" "kne-image" {
  project_id            = "gep-kne"
  source_image          = "debian-11-bullseye-v20210817"
  disk_size             = 50
  image_name            = "kne-{{timestamp}}"
  image_family          = "kne"
  image_labels          = { "tested" : "false" }
  image_description     = "Debian based linux VM image with KNE and all dependencies installed."
  ssh_username          = "user"
  machine_type          = "e2-standard-4"
  zone                  = "us-central1-a"
  service_account_email = "packer@gep-kne.iam.gserviceaccount.com"
  use_internal_ip       = true
}

build {
  name    = "kne-builder"
  sources = ["sources.googlecompute.kne-image"]

  provisioner "shell" {
    inline = [
      "echo Installing golang...",
      "curl -O https://dl.google.com/go/go1.16.5.linux-amd64.tar.gz",
      "sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.16.5.linux-amd64.tar.gz",
      "echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.bashrc",
      "echo 'export PATH=$PATH:$(go env GOPATH)/bin' >> ~/.bashrc",
      "/usr/local/go/bin/go version",
    ]
  }

  provisioner "shell" {
    inline = [
      "echo Installing docker...",
      "sudo apt-get update",
      "sudo apt-get install apt-transport-https ca-certificates curl gnupg lsb-release -y",
      "curl -fsSL https://download.docker.com/linux/debian/gpg | sudo gpg --dearmor -o /usr/share/keyrings/docker-archive-keyring.gpg",
      "echo \"deb [arch=amd64 signed-by=/usr/share/keyrings/docker-archive-keyring.gpg] https://download.docker.com/linux/debian $(lsb_release -cs) stable\" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null",
      "sudo apt-get update",
      "sudo apt-get install docker-ce docker-ce-cli containerd.io build-essential -y",
      "sudo usermod -aG docker $USER",
      "docker -v",
      "gcloud auth configure-docker -q",
      "docker pull kindest/node",
      "docker pull networkop/init-wait",
      "docker pull networkop/meshnet",
    ]
  }

  provisioner "shell" {
    inline = [
      "echo Installing kind...",
      "/usr/local/go/bin/go get -u sigs.k8s.io/kind",
      "/home/$USER/go/bin/kind version",
    ]
  }

  provisioner "shell" {
    inline = [
      "echo Installing kubectl...",
      "curl -LO \"https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl\"",
      "sudo install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl",
      "kubectl version --client",
    ]
  }

  provisioner "shell" {
    inline = [
      "echo Cloning google/kne...",
      "sudo apt-get install git -y",
      "git clone https://github.com/google/kne.git",
      "cd kne/kne_cli",
      "/usr/local/go/bin/go build -v",
    ]
  }
}

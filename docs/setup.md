# Setup KNE

This is part of the How-To guide collection. This guide covers KNE first time
setup on a Linux machine. Specifically the `Ubuntu Focal 20.04 (LTS)` base
image. Modifications may need to be made to the commands listed below in order
to work with your Linux distro.

The following dependencies and required to use KNE:

* Golang
* Docker
* Kubectl
* Kind
* Make

## Install Golang

1. If golang is already installed then check the version using `go version`. If `1.17` or newer then golang installation is complete.

1. Install the new version:

  ```bash
  curl -O https://dl.google.com/go/go1.17.7.linux-amd64.tar.gz
  sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.17.7.linux-amd64.tar.gz
  rm go1.17.7.linux-amd64.tar.gz
  export PATH=$PATH:/usr/local/go/bin
  export PATH=$PATH:$(go env GOPATH)/bin
  ```

  Consider adding the `export` commands to `~/.bashrc` or similar.

## Install Docker

NOTE: This will install version `20.10.16` which was known to work with KNE at
some point in time. You can instead install a newer version if you need new
features or are having problems.

1. Follow the installation [instructions](https://docs.docker.com/engine/install/ubuntu/#install-using-the-repository) using version string `5:20.10.16~3-0~ubuntu-focal` or the equivalent `20.10.16` version for the corresponding OS.

1. Follow the
    [post-installation steps](https://docs.docker.com/engine/install/linux-postinstall/#manage-docker-as-a-non-root-user)
    for Linux. Specifically
    [manager docker as a non-root user](https://docs.docker.com/engine/install/linux-postinstall/#manage-docker-as-a-non-root-user).

    ```bash
    sudo groupadd docker
    sudo usermod -aG docker $USER
    ```

## Install Kubectl

NOTE: This will install version `1.24.1` which was known to work with KNE at
some point in time. You can instead install a newer version if you need new
features or are having problems.

```bash
curl -LO https://dl.k8s.io/release/v1.24.1/bin/linux/amd64/kubectl
sudo install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl
```

## Install Kind

NOTE: This will install version `0.14.0` which was known to work with KNE at
some point in time. You can instead install a newer version if you need new
features or are having problems.

```bash
go install sigs.k8s.io/kind@v0.14.0
```

## Clone openconfig/kne GitHub repo

Clone the repo:

```bash
git clone https://github.com/openconfig/kne.git
```

Install the `kne` binary:

```bash
make install
```

This will build the `kne` binary and move it to `/usr/local/bin` (which should be in your
`$PATH`). Now run:

```bash
kne help
```

To verify that the `kne` is built and accessible from your `$PATH`.

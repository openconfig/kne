# Setup KNE

This is part of the How-To guide collection. This guide covers KNE first time
setup on a Linux machine. The guide has been verified on a Linux host with Debian 6.10.11-1rodete2. 
Modifications may need to be made to the commands listed below in order
to work with your Linux distribution.

The following dependencies and required to use KNE:

* Golang
* Docker
* Kubectl
* Kind
* Make

## Install Golang

1. If golang is already installed then check the version using `go version`. If
   `1.23` or newer then golang installation is complete. Otherwise please follow the [instructions] (https://go.dev/doc/install) 
   to install golang.
   Please export GOPATH and add the export commands to ~/.bashrc or similar.

   ```bash
   export PATH=$PATH:$(go env GOPATH)/bin
   ```


## Install Docker

> NOTE: This will install version `20.10.21` which was known to work with KNE at
> some point in time. You can instead install a newer version if you need new
> features or are having problems.

1. Follow the installation
   [instructions](https://docs.docker.com/engine/install/debian)

2. Follow the [post-installation
   steps](https://docs.docker.com/engine/install/linux-postinstall/#manage-docker-as-a-non-root-user)
   for Linux. Specifically [manager docker as a non-root
   user](https://docs.docker.com/engine/install/linux-postinstall/#manage-docker-as-a-non-root-user).

   ```bash
   sudo groupadd docker
   sudo usermod -aG docker $USER
   ```

## Install kubectl

> NOTE: This will install version `1.32.3` which was known to work with KNE at
> some point in time. You can instead install a newer version if you need new
> features or are having problems.
1. Follow the installation:
   [instructions](https://kubernetes.io/docs/tasks/tools/install-kubectl-linux/)

```bash
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
sudo install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl
```

## Install Kind

> NOTE: This will install version `0.27.0` which was known to work with KNE at
> some point in time. You can instead install a newer version if you need new
> features or are having problems.

```bash
go install sigs.k8s.io/kind@v0.27.0
```

## Clone openconfig/kne GitHub repo

Clone the repo:

```bash
git clone https://github.com/openconfig/kne.git
```

Install the `kne` binary:

```bash
cd kne
make install
```

This will build the `kne` binary and move it to `/usr/local/bin` (which should
be in your `$PATH`). Now run:

```bash
kne help
```

To verify that the `kne` is built and accessible from your `$PATH`.

# Setup KNE

This is part of the How-To guide collection. This guide covers KNE first time
setup on a Linux machine. Specifically the `Ubuntu Focal 20.04 (LTS)` base
image. Modifications may need to be made to the commands listed below in order
to work with your Linux distro.

The following dependencies and required to use KNE:

*   Docker
*   Kind
*   Go
*   Kubectl

## Install Docker

1.  Follow the instructions at https://docs.docker.com/engine/install/

1.  Follow the
    [post-installation steps](https://docs.docker.com/engine/install/linux-postinstall/#manage-docker-as-a-non-root-user)
    for Linux. Specifically
    [manager docker as a non-root user](https://docs.docker.com/engine/install/linux-postinstall/#manage-docker-as-a-non-root-user).

    ```bash
    $ sudo groupadd docker
    $ sudo usermod -aG docker $USER
    ```

## Install kubectl

```bash
$ curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
$ chmod +x kubectl
$ mkdir -p ~/go/bin
$ mv kubectl ~/go/bin/kubectl
```

## Install kind

```bash
$ go install sigs.k8s.io/kind@latest
```

## Clone openconfig/kne GitHub repo

Clone the repo:

```bash
$ git clone https://github.com/openconfig/kne.git
```

Build the `kne_cli`:

```bash
$ cd kne/kne_cli
$ go install
```

This will build the `kne_cli` in your `$GOPATH/bin` (which should be in your
`$PATH`). Now run:

```bash
$ kne_cli --help
```

To verify that the `kne_cli` is built and accessible from your `$PATH`.

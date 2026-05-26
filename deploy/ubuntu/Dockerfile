FROM ubuntu:24.04

RUN apt-get update && apt-get install -y --no-install-recommends \
  curl \
  wget \
  iproute2 \
  iputils-ping \
  tcpdump \
  telnet \
  traceroute \
  ca-certificates \
  && rm -rf /var/lib/apt/lists/*

RUN curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" \
&& chmod +x kubectl \
&& mv kubectl /usr/local/bin/kubectl

RUN groupadd -r kne && useradd -r -g kne -s /sbin/nologin kne
USER kne

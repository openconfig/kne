FROM ubuntu:latest

RUN apt-get update && apt-get install -y \
  curl \
  wget \
  iproute2 \
  iputils-ping \
  tcpdump \
  telnet \
  traceroute \
  && rm -rf /var/lib/apt/lists/*

RUN curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl" \
&& chmod +x kubectl \
&& mv kubectl /usr/local/bin/kubectl

FROM ubuntu:latest

RUN apt-get update && apt-get install -y \
  iproute2 \
  iputils-ping \
  tcpdump \
  traceroute \
  && rm -rf /var/lib/apt/lists/*

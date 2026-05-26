FROM ubuntu:24.04

RUN apt-get update && apt-get install -y --no-install-recommends \
  curl \
  ca-certificates \
  && rm -rf /var/lib/apt/lists/*

RUN curl -LO "https://github.com/osrg/gobgp/releases/download/v2.31.0/gobgp_2.31.0_linux_amd64.tar.gz" \
&& tar -xzf gobgp_2.31.0_linux_amd64.tar.gz && chmod +x gobgpd && chmod +x gobgp \
&& mv gobgpd /usr/local/bin/gobgpd && mv gobgp /usr/local/bin/gobgp \
&& rm -f gobgp_2.31.0_linux_amd64.tar.gz

RUN groupadd -r kne && useradd -r -g kne -s /sbin/nologin kne
USER kne

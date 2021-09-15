FROM hfam/ubuntu:latest

RUN curl -LO "https://github.com/osrg/gobgp/releases/download/v2.31.0/gobgp_2.31.0_linux_amd64.tar.gz" \
&& tar -xzf gobgp_2.31.0_linux_amd64.tar.gz && chmod +x gobgpd  && chmod +x gobgp \
&& mv gobgpd /usr/local/bin/gobgpd && mv gobgp /usr/local/bin/gobgp

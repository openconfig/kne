#!/bin/bash

echo "Distributing files"
if [ -d "/opt/cni/bin/" ] && [ -f "./meshnet" ]; then
  cp ./meshnet /opt/cni/bin/
fi

if [ -d "/etc/cni/net.d/" ] && [ -f "./meshnet.conf" ]; then
  cp ./meshnet.conf /etc/cni/net.d/
fi


echo "Starting meshnetd daemon"
/meshnetd

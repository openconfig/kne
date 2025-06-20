#!/bin/sh

INTFS=${1:-1}
SLEEP=${2:-0}
DISABLE_IPV6=${3:-0}

int_calc () 
{
    index=0
    # shellcheck disable=2010
    for i in $(ls -1v /sys/class/net/ | grep 'eth\|ens\|eno\|^e[0-9]'); do
      # shellcheck disable=3039
      let index=index+1
    done
    MYINT=$index
}

int_calc

echo "Waiting for all $INTFS interfaces to be connected"
while [ "$MYINT" -lt "$INTFS" ]; do
  echo "Connected $MYINT interfaces out of $INTFS"
  sleep 1
  int_calc
done

if [ "$DISABLE_IPV6" -ne 0 ]; then
    # shellcheck disable=2010
    for i in $(ls -1v /sys/class/net/ | grep 'eth\|ens\|eno\|^e[0-9]'); do
      sysctl net.ipv6.conf."$i".disable_ipv6=1
      ip link set dev "$i" arp off
    done
fi

echo "Sleeping $SLEEP seconds before boot"
sleep "$SLEEP"

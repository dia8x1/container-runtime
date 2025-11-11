#!/bin/bash

set -e

BRIDGE_NAME="cr0"
BRIDGE_SUBNET="172.19.0.0/24"
BRIDGE_IP="172.19.0.1/24"

if ! ip link show $BRIDGE_NAME > /dev/null 2>&1; then
  ip link add name $BRIDGE_NAME type bridge
  ip addr add $BRIDGE_IP dev $BRIDGE_NAME
  ip link set $BRIDGE_NAME up

  sysctl -w net.ipv4.ip_forward=1

  iptables -t nat -A POSTROUTING -s $BRIDGE_SUBNET ! -o $BRIDGE_NAME -j MASQUERADE

  echo "Bridge $BRIDGE_NAME created with subnet $BRIDGE_SUBNET"
else
  echo "Bridge $BRIDGE_NAME already exists"
fi
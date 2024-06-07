#!/bin/bash
#
# Decrypt flows_cred.json from a NodeRED data directory
#
# Usage
# ./node-red-decrypt-flows-cred.sh ./node_red_data
#
jq  '.["$"]' -j $1/flows_cred.json | \
  cut -c 33- | \
  openssl enc -aes-256-ctr -d -base64 -A -iv `jq  -r '.["$"]' $1/flows_cred.json | cut -c 1-32` -K `jq -j '._credentialSecret' $1/.config.runtime.json | sha256sum | cut -c 1-64`
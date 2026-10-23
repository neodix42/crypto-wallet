#!/bin/sh
# Pinned public testing tool; never handles wallet material.
set -eu
mkdir -p .tools/aptos
curl -fL --retry 2 -o .tools/aptos/cli.zip https://github.com/aptos-labs/aptos-core/releases/download/aptos-cli-v7.11.0/aptos-cli-7.11.0-Ubuntu-24.04-x86_64.zip
printf '%s\n' '2fd176882391efce2d89e2a794f0ed476bdb61335154052874a1884dbf4da947  .tools/aptos/cli.zip' | sha256sum -c -
unzip -o .tools/aptos/cli.zip -d .tools/aptos
printf '%s\n' '7da14e8cceced2b10834856fae2f6b26095275a4da488ca2c5215691daa37e90  .tools/aptos/aptos' | sha256sum -c -

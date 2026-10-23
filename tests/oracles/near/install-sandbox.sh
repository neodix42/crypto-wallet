#!/bin/sh
# Exact public native test tool, never production wallet key storage.
set -eu
mkdir -p .tools/near
curl -fL --retry 2 -o .tools/near/sandbox.tar.gz https://s3-us-west-1.amazonaws.com/build.nearprotocol.com/nearcore/Linux-x86_64/2.13.4/near-sandbox.tar.gz
printf '%s\n' '5690a4635172263cf026683f75965cac09dd916912584f12709974fed620163f  .tools/near/sandbox.tar.gz' | sha256sum -c -
tar -xzf .tools/near/sandbox.tar.gz --strip-components=1 -C .tools/near
printf '%s\n' '4e89fbc0f2958634e84af44634f90e87c246288fa11b067d2121ce1394cba2c2  .tools/near/near-sandbox' | sha256sum -c -

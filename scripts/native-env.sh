#!/bin/sh
# Source from the repository root; only the invoking build receives these paths.
wallet_native_root="$PWD/.tools/native"
wallet_multiarch="$(cc -print-multiarch)"
if [ -d "$wallet_native_root/usr" ]; then
  export PKG_CONFIG_PATH="$wallet_native_root/usr/lib/$wallet_multiarch/pkgconfig:$wallet_native_root/usr/lib/pkgconfig:$wallet_native_root/usr/share/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
  export CPATH="$wallet_native_root/usr/include:$wallet_native_root/usr/include/$wallet_multiarch${CPATH:+:$CPATH}"
  export LIBRARY_PATH="$wallet_native_root/usr/lib/$wallet_multiarch:$wallet_native_root/usr/lib${LIBRARY_PATH:+:$LIBRARY_PATH}"
fi
unset wallet_native_root wallet_multiarch

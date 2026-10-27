#!/bin/sh
set -eu
Xvfb :99 -screen 0 1280x1024x24 -nolisten tcp >/tmp/xvfb.log 2>&1 &
display_pid=$!
trap 'kill "$display_pid" 2>/dev/null || true' EXIT
sleep 1
# Exercise actual packaged native initialization under production seccomp.
/opt/cryptowallet/resources/bin/wallet-signer --data /home/wallet/native-probe --environment testnet </dev/null
# No sandbox bypass flags, root application launch, or host policy changes.
dbus-run-session -- /opt/cryptowallet/cryptowallet --ozone-platform=x11 >/tmp/wallet.log 2>&1 &
app_pid=$!
sleep 8
if ! kill -0 "$app_pid" 2>/dev/null; then
  cat /tmp/wallet.log
  exit 1
fi
xdotool search --name 'CryptoWallet' >/tmp/windows
test -s /tmp/windows
printf '%s\n' 'PASS packaged non-root X11 startup with sandbox and native signer initialization'
kill "$app_pid"

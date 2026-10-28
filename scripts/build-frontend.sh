#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
printf 'Building the embedded React/TypeScript interface...\n'
npm --prefix internal/webui/frontend ci --ignore-scripts --no-audit --no-fund
npm --prefix internal/webui/frontend run build

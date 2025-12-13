#!/usr/bin/env bash
set -euo pipefail
if ! command -v shfmt >/dev/null 2>&1; then
  echo "shfmt missing; install via 'go install mvdan.cc/sh/v3/cmd/shfmt@latest'" >&2
  exit 1
fi
if [ $# -ne 1 ]; then
  echo "Usage: $0 <file>" >&2
  exit 2
fi
shfmt -w "$1"

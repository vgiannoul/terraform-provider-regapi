#!/usr/bin/env bash
# Copies RegAPI's openapi.yaml into the fake API's testdata, noting the commit it came from.
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <path to a RegAPI checkout>" >&2
  exit 2
fi
checkout="$1"
commit="$(git -C "${checkout}" rev-parse --short HEAD)"
target="$(dirname "$0")/../internal/fakeapi/testdata/openapi.yaml"
{
  echo "# Vendored from the RegAPI repository at ${commit}; update with scripts/vendor-openapi.sh."
  cat "${checkout}/platform/internal/api/openapi.yaml"
} >"${target}"
echo "vendored openapi.yaml at ${commit}"

#!/usr/bin/env bash
# Rewrites each `uses: owner/repo@vX` in the workflows to `uses: owner/repo@<sha> # vX`.
# Needs an authenticated gh. Already pinned lines are left alone.
set -euo pipefail

cd "$(dirname "$0")/.."
for file in .github/workflows/*.yml; do
  while IFS= read -r ref; do
    action="${ref%@*}"
    tag="${ref#*@}"
    sha="$(gh api "repos/${action}/commits/${tag}" --jq .sha)"
    sed -i.bak "s|${action}@${tag}\$|${action}@${sha} # ${tag}|" "${file}"
    rm -f "${file}.bak"
    echo "${file}: ${action} ${tag} -> ${sha}"
  done < <(grep -oE 'uses: [^ ]+@v[0-9][^ ]*$' "${file}" | sed 's/^uses: //')
done

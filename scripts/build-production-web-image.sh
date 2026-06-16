#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

image="${1:-}"
if [[ -z "$image" ]]; then
  echo "usage: $0 <image-tag>" >&2
  exit 2
fi

branch="$(git rev-parse --abbrev-ref HEAD)"
if [[ "$branch" != "main" ]]; then
  echo "production image builds must run from main; current branch is $branch" >&2
  exit 1
fi

git diff --quiet
git diff --cached --quiet

docker build \
  --label org.opencontainers.image.ref.name=main \
  --label multica.production-trunk=main \
  -t "$image" \
  -f Dockerfile.web \
  .

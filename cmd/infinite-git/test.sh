#!/usr/bin/env bash

set -eux

dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT

git clone https://infinite-git.kontain.me/ "$dir/repo"
cd "$dir/repo"
test -f hello.txt

before=$(git rev-parse HEAD)
git pull
after=$(git rev-parse HEAD)

if [ "$before" = "$after" ]; then
    echo "expected a new commit, but HEAD didn't move ($before)"
    exit 1
fi

git ls-remote http://infinite-git.kontain.me
git ls-remote http://infinite-git.kontain.me

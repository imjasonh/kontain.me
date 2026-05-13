#!/usr/bin/env bash

set -eux

# go-get discovery returns a meta tag pointing at the module's git URL.
curl -fsSL "https://infinite-go.kontain.me/?go-get=1" | grep -F 'name="go-import"'

# It also serves a real git repo, so clone+pull produces new content.
dir=$(mktemp -d)
trap 'rm -rf "$dir"' EXIT

git clone https://infinite-go.kontain.me/ "$dir/repo"
cd "$dir/repo"
test -f go.mod
test -f pulltime.go

before=$(git rev-parse HEAD)
git pull
after=$(git rev-parse HEAD)

if [ "$before" = "$after" ]; then
    echo "expected a new commit, but HEAD didn't move ($before)"
    exit 1
fi

go get infinite-git.kontain.me@latest
go get infinite-git.kontain.me@latest

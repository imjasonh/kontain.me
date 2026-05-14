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

# `go get` needs to run from a module that isn't infinite-go.kontain.me itself.
modtest=$(mktemp -d)
cd "$modtest"
cat > go.mod <<EOF
module test

go 1.24
EOF
GOPROXY=direct go get infinite-go.kontain.me@latest
GOPROXY=direct go get infinite-go.kontain.me@latest
rm -rf "$modtest"

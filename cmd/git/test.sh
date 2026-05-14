#!/usr/bin/env bash

set -eux

time crane validate --remote=git.kontain.me/github.com/imjasonh/kontain.me:main
# Default tag resolves to the default branch (main): same commit, cache hit.
time crane validate --remote=git.kontain.me/github.com/imjasonh/kontain.me

docker run --rm --pull=always git.kontain.me/github.com/imjasonh/kontain.me -c "git log -1"
docker run --rm --pull=always git.kontain.me/github.com/google/go-containerregistry -c "git fetch --unshallow && git log"

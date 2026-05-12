#!/usr/bin/env bash

set -eux

time crane validate --remote=git.kontain.me/github.com/imjasonh/kontain.me:main
# Default tag resolves to the default branch (main): same commit, cache hit.
time crane validate --remote=git.kontain.me/github.com/imjasonh/kontain.me

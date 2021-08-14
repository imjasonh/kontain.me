#!/usr/bin/env bash

set -euxo pipefail

time crane validate --remote=random.kontain.me/random
time crane validate --remote=random.kontain.me/random:4x10

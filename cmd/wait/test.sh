#!/usr/bin/env bash

set -euxo pipefail

uid=image-$(date +%s)
crane manifest wait.kontain.me/${uid} || true
sleep 11 # for good measure
time crane validate --remote=wait.kontain.me/${uid}
time crane validate --remote=wait.kontain.me/${uid}

#!/usr/bin/env bash

set -euxo pipefail

find cmd -name 'deploy.sh' -print0 | 
    while IFS= read -r -d '' line; do 
        bash -c "$line"
    done

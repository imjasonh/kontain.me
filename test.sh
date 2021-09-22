#!/bin/bash

set -euxo pipefail

find cmd -n 'test.sh' -print0 |
    while IFS= read -r -d '' line; do 
        bash -c "$line"
    done

#!/bin/bash

set -euxo pipefail

find cmd -name 'test.sh' -print0 |
    while IFS= read -r -d '' line; do 
        bash -c "$line"
    done

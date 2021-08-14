#!/bin/bash

set -euxo pipefail

time find cmd -name "test.sh" -exec {} \;

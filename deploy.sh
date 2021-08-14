#!/usr/bin/env bash

set -euxo pipefail

time find cmd -name "deploy.sh" -exec {} \;

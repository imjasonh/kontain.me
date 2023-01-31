#!/usr/bin/env bash

set -euxo pipefail

time crane validate --remote=apko.kontain.me/kubectl
time crane validate --remote=apko.kontain.me/kubectl


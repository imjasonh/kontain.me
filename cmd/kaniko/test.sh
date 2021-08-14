#!/usr/bin/env bash

set -euxo pipefail

time crane validate --remote=kaniko.kontain.me/dockersamples/node-bulletin-board/bulletin-board-app:3f08afd
time crane validate --remote=kaniko.kontain.me/dockersamples/node-bulletin-board/bulletin-board-app:3f08afd


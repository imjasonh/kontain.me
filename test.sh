#!/bin/bash

set -euxo pipefail

docker pull random.kontain.me/random
docker pull random.kontain.me/random:8x80

docker pull kontain.me/ko/github.com/knative/build/cmd/controller

docker pull cnb.kontain.me/buildpack/sample-java-app

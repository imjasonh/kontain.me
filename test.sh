#!/bin/bash

set -euxo pipefail

curl https://kontain.me | grep html

time docker pull random.kontain.me/random
time docker pull random.kontain.me/random:8x80

time docker pull ko.kontain.me/ko/github.com/knative/build/cmd/controller

time docker pull buildpack.kontain.me/buildpack/sample-java-app
time docker pull buildpack.kontain.me/buildpack/sample-java-app # caching! # caching! # caching!

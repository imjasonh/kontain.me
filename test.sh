#!/bin/bash

set -euxo pipefail

curl https://kontain.me | grep html

time docker pull random.kontain.me/random
time docker pull random.kontain.me/random:8x80

time docker pull ko.kontain.me/ko/github.com/knative/build/cmd/controller

time docker pull buildpack.kontain.me/buildpack/samples/apps/java-maven
time docker pull buildpack.kontain.me/buildpack/samples/apps/java-maven # caching!

tmp=$(mktemp -d)
git clone git@github.com:buildpack/samples/ $tmp
project=$(gcloud config get-value project)
CLOUDSDK_API_ENDPOINT_OVERRIDES_CLOUDBUILD=https://api-an3qnndwmq-uc.a.run.app/ gcloud builds submit --tag=gcr.io/$project/built $tmp/apps/java-maven
rm $tmp

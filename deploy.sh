#!/bin/bash

set -euxo pipefail

docker build -t gcr.io/kontainme/graphviz -f cmd/viz/Dockerfile cmd/viz/
docker push gcr.io/kontainme/graphviz
KO_DOCKER_REPO=gcr.io/kontainme ko publish -P ./cmd/viz && \
gcloud --project=kontainme beta run deploy viz \
  --image=gcr.io/kontainme/github.com/imjasonh/kontain.me/cmd/viz \
  --region=us-central1

KO_DOCKER_REPO=gcr.io/kontainme ko publish -P ./cmd/api && \
gcloud --project=kontainme beta run deploy api \
  --image=gcr.io/kontainme/github.com/imjasonh/kontain.me/cmd/api \
  --memory=2Gi \
  --concurrency=1 \
  --region=us-central1

KO_DOCKER_REPO=gcr.io/kontainme ko publish -P ./cmd/app && \
gcloud --project=kontainme beta run deploy app \
  --image=gcr.io/kontainme/github.com/imjasonh/kontain.me/cmd/app \
  --region=us-central1

KO_DOCKER_REPO=gcr.io/kontainme ko publish -P ./cmd/random && \
gcloud --project=kontainme beta run deploy random \
  --image=gcr.io/kontainme/github.com/imjasonh/kontain.me/cmd/random \
  --region=us-central1

KO_DOCKER_REPO=gcr.io/kontainme ko publish -P ./cmd/ko && \
gcloud --project=kontainme beta run deploy ko \
  --image=gcr.io/kontainme/github.com/imjasonh/kontain.me/cmd/ko \
  --memory=2Gi \
  --concurrency=1 \
  --region=us-central1

KO_DOCKER_REPO=gcr.io/kontainme ko publish -P ./cmd/buildpack && \
gcloud --project=kontainme beta run deploy buildpack \
  --image=gcr.io/kontainme/github.com/imjasonh/kontain.me/cmd/buildpack \
  --memory=2Gi \
  --concurrency=1 \
  --region=us-central1

#!/bin/bash

KO_DOCKER_REPO=gcr.io/kontainme ko publish -P ./cmd/ko && \
gcloud --project=kontainme beta run deploy app \
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

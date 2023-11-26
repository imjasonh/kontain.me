#!/usr/bin/env bash

set -eux

gcloud run deploy viz \
  --project=kontaindotme \
  --region=us-central1 \
  --allow-unauthenticated \
  --set-env-vars=BUCKET=kontaindotme \
  --image=$(KO_DOCKER_REPO=gcr.io/kontaindotme ko publish -P ./cmd/viz) \
  --memory=1Gi \
  --cpu=1 \
  --concurrency=80 \
  --timeout=60 # 1m

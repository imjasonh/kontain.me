#!/usr/bin/env bash

set -euxo pipefail

gcloud run deploy buildpack \
  --project=kontaindotme \
  --region=us-central1 \
  --allow-unauthenticated \
  --set-env-vars=BUCKET=kontaindotme \
  --image=$(KO_DOCKER_REPO=gcr.io/kontaindotme ko publish -P ./cmd/buildpack) \
  --memory=4Gi \
  --cpu=2 \
  --concurrency=1 \
  --timeout=900 # 15m

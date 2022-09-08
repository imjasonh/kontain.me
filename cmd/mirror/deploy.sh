#!/usr/bin/env bash

set -euxo pipefail

gcloud run deploy mirror \
  --project=kontaindotme \
  --region=us-central1 \
  --allow-unauthenticated \
  --set-env-vars=BUCKET=kontaindotme \
  --image=$(KO_DOCKER_REPO=gcr.io/kontaindotme ko publish -P ./cmd/mirror) \
  --memory=4Gi \
  --cpu=4 \
  --concurrency=80 \
  --timeout=600 # 10m

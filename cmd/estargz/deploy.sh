#!/usr/bin/env bash

set -euxo pipefail

gcloud run deploy estargz \
  --project=kontaindotme \
  --region=us-central1 \
  --allow-unauthenticated \
  --set-env-vars=BUCKET=kontaindotme \
  --image=$(KO_DOCKER_REPO=gcr.io/kontaindotme ko publish -P ./cmd/estargz) \
  --memory=4Gi \
  --cpu=1 \
  --concurrency=80 \
  --timeout=300 # 5m

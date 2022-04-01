#!/usr/bin/env bash

set -euxo pipefail

gcloud beta run deploy apko \
  --project=kontaindotme \
  --region=us-central1 \
  --allow-unauthenticated \
  --set-env-vars=BUCKET=kontaindotme \
  --image=$(KO_DOCKER_REPO=gcr.io/kontaindotme ko publish -P ./cmd/apko) \
  --memory=512Mi \
  --cpu=1 \
  --concurrency=1 \
  --execution-environment gen2 \
  --timeout=900 # 15m

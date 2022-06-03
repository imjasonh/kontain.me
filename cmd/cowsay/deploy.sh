#!/usr/bin/env bash

set -euxo pipefail

gcloud run deploy cowsay \
  --project=kontaindotme \
  --region=us-central1 \
  --allow-unauthenticated \
  --set-env-vars=BUCKET=kontaindotme \
  --image=$(KO_DOCKER_REPO=gcr.io/kontaindotme ko publish -P ./cmd/cowsay) \
  --memory=256Mi \
  --cpu=1 \
  --concurrency=80 \
  --timeout=60 # 1m

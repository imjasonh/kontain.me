#!/usr/bin/env bash

set -euxo pipefail

gcloud run deploy transparency \
  --project=kontaindotme \
  --region=us-central1 \
  --allow-unauthenticated \
  --image=$(KO_DOCKER_REPO=gcr.io/kontaindotme ko publish -P ./cmd/transparency) \
  --memory=128Mi \
  --cpu=1 \
  --concurrency=1000 \
  --timeout=60

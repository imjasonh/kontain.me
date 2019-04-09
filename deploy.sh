#!/bin/bash

# Deploy backend
KO_DOCKER_REPO=gcr.io/kontainme ko publish -P . && \
gcloud --project=kontainme beta run deploy app \
  --image=gcr.io/kontainme/github.com/imjasonh/kontain.me \
  --memory=2Gi \
  --concurrency=1 \
  --region=us-central1

# Deploy forwarding frontend
gcloud --project=kontainme app deploy -q fwd/

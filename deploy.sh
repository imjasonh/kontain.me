#!/bin/bash

set -euxo pipefail

project=kontainme
region=us-central1

case ${1:-"all"} in

  viz | all)
    docker build -t gcr.io/${project}/graphviz -f cmd/viz/Dockerfile cmd/viz/
    docker push gcr.io/${project}/graphviz
    KO_DOCKER_REPO=gcr.io/${project} ko publish -P ./cmd/viz && \
    gcloud --project=${project} run deploy viz \
      --image=gcr.io/${project}/github.com/imjasonh/kontain.me/cmd/viz \
      --region=${region} \
      --platform=managed
    ;;

  api | all)
    KO_DOCKER_REPO=gcr.io/${project} ko publish -P ./cmd/api && \
    gcloud --project=${project} run deploy api \
      --image=gcr.io/${project}/github.com/imjasonh/kontain.me/cmd/api \
      --memory=2Gi \
      --concurrency=1 \
      --region=${region} \
      --platform=managed
    ;;

  app | all)
    KO_DOCKER_REPO=gcr.io/${project} ko publish -P ./cmd/app && \
    gcloud --project=${project} run deploy app \
      --image=gcr.io/${project}/github.com/imjasonh/kontain.me/cmd/app \
      --region=${region} \
      --platform=managed
    ;;

  random | all)
    KO_DOCKER_REPO=gcr.io/${project} ko publish -P ./cmd/random && \
    gcloud --project=${project} run deploy random \
      --image=gcr.io/${project}/github.com/imjasonh/kontain.me/cmd/random \
      --region=${region} \
      --platform=managed
    ;;

  ko | all)
    KO_DOCKER_REPO=gcr.io/${project} ko publish -P ./cmd/ko && \
    gcloud --project=${project} run deploy ko \
      --image=gcr.io/${project}/github.com/imjasonh/kontain.me/cmd/ko \
      --memory=2Gi \
      --concurrency=1 \
      --region=${region} \
      --platform=managed
    ;;

  buildpack | all)
    KO_DOCKER_REPO=gcr.io/${project} ko publish -P ./cmd/buildpack && \
    gcloud --project=${project} run deploy buildpack \
      --image=gcr.io/${project}/github.com/imjasonh/kontain.me/cmd/buildpack \
      --memory=2Gi \
      --concurrency=1 \
      --region=${region} \
      --platform=managed
    ;;

  mirror | all)
    KO_DOCKER_REPO=gcr.io/${project} ko publish -P ./cmd/mirror && \
    gcloud --project=${project} run deploy mirror \
      --image=gcr.io/${project}/github.com/imjasonh/kontain.me/cmd/mirror \
      --memory=2Gi \
      --region=${region} \
      --platform=managed
    ;;
esac

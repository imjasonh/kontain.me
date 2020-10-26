#!/bin/bash

set -euxo pipefail

project=kontainme
region=us-central1

function deploy(){
    service=$1
    memory=${2:-1Gi}
    cpu=${3:-1}
    concurrency=${4:-80}
    timeout=${5:-5m}

    KO_DOCKER_REPO=gcr.io/${project} ko publish -P ./cmd/${service} && \
    gcloud --project=${project} run deploy ${service} \
      --image=gcr.io/${project}/github.com/imjasonh/kontain.me/cmd/${service} \
      --memory=${memory} \
      --cpu=${cpu} \
      --concurrency=${concurrency} \
      --timeout=${timeout} \
      --region=${region} \
      --platform=managed
}

function deploy_api()       { deploy api       2Gi 1 1 5m  ;}
function deploy_app()       { deploy app                   ;}
function deploy_buildpack() { deploy buildpack 4Gi 2 1 15m ;}
function deploy_ko()        { deploy ko        4Gi 2 1 15m ;}
function deploy_mirror()    { deploy mirror                ;}
function deploy_random()    { deploy random                ;}
function deploy_viz()       { deploy viz                   ;}

function build_viz_base() {
    docker build -t gcr.io/${project}/graphviz -f cmd/viz/Dockerfile cmd/viz/
    docker push gcr.io/${project}/graphviz
}

case ${1:-"all"} in
  api)       deploy_api;;
  app)       deploy_app;;
  buildpack) deploy_buildpack;;
  ko)        deploy_ko;;
  mirror)    deploy_mirror;;
  random)    deploy_random;;
  viz)       deploy_viz;;
  viz_base)  build_viz_base; deploy_viz;;

  all)
    deploy_api
    deploy_app
    deploy_buildpack
    deploy_ko
    deploy_mirror
    deploy_random
    build_viz_base; deploy_viz
    ;;

esac

#!/usr/bin/env bash

set -euxo pipefail

time crane validate --remote=ko.kontain.me/knative.dev/serving/cmd/controller:latest
time crane validate --remote=ko.kontain.me/knative.dev/serving/cmd/controller:latest

time crane validate --remote=ko.kontain.me/knative.dev/serving/cmd/controller:v0.26.0
time crane validate --remote=ko.kontain.me/knative.dev/serving/cmd/controller:main

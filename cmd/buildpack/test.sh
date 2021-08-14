#!/usr/bin/env bash

set -euxo pipefail

time crane validate --remote=buildpack.kontain.me/googlecloudplatform/buildpack-samples/sample-go:ed393d
time crane validate --remote=buildpack.kontain.me/googlecloudplatform/buildpack-samples/sample-go:ed393d

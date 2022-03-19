#!/usr/bin/env bash

set -euxo pipefail

time crane validate --remote=ko.kontain.me/github.com/google/ko/test:latest
time crane validate --remote=ko.kontain.me/github.com/google/ko/test:latest

time crane validate --remote=ko.kontain.me/github.com/google/ko/test:v0.10.0
time crane validate --remote=ko.kontain.me/github.com/google/ko/test:main

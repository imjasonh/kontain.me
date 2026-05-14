#!/usr/bin/env bash

set -eux

time crane validate --remote=ko.kontain.me/github.com/google/ko/test:latest
time crane validate --remote=ko.kontain.me/github.com/google/ko/test:latest

#!/usr/bin/env bash

set -eux

time crane validate --remote=flatten.kontain.me/cgr.dev/chainguard/busybox:latest-glibc
time crane validate --remote=flatten.kontain.me/cgr.dev/chainguard/busybox:latest-glibc

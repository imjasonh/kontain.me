#!/bin/bash

set -euxo pipefail

### buildpack

time crane validate --remote=buildpack.kontain.me/googlecloudplatform/buildpack-samples/sample-go:ed393d
time crane validate --remote=buildpack.kontain.me/googlecloudplatform/buildpack-samples/sample-go:ed393d

### flatten

time crane validate --remote=flatten.kontain.me/busybox
time crane validate --remote=flatten.kontain.me/busybox

### kaniko

time crane validate --remote=kaniko.kontain.me/dockersamples/node-bulletin-board/bulletin-board-app:3f08afd
time crane validate --remote=kaniko.kontain.me/dockersamples/node-bulletin-board/bulletin-board-app:3f08afd

### ko

#time crane validate --remote=ko.kontain.me/github.com/google/ko/cmd/ko
#time crane validate --remote=ko.kontain.me/github.com/google/ko/cmd/ko

### mirror

time crane validate --remote=mirror.kontain.me/busybox
time crane validate --remote=mirror.kontain.me/busybox

### random

time crane validate --remote=random.kontain.me/random
time crane validate --remote=random.kontain.me/random:4x10

### viz

curl https://viz.kontain.me | grep textarea

### wait

uid=image-$(date +%s)
crane manifest wait.kontain.me/${uid} || true
sleep 11 # for good measure
time crane validate --remote=wait.kontain.me/${uid}
time crane validate --remote=wait.kontain.me/${uid}

# kontain.me

[![CI](https://github.com/imjasonh/kontain.me/actions/workflows/ci.yaml/badge.svg?event=push)](https://github.com/imjasonh/kontain.me/actions/workflows/ci.yaml)
[![Deploy](https://github.com/imjasonh/kontain.me/actions/workflows/deploy.yaml/badge.svg)](https://github.com/imjasonh/kontain.me/actions/workflows/deploy.yaml)

Serving container images generated on-demand, at the time they are requested.

These include:

* [`random.kontain.me`](./cmd/random), which serves randomly-generated images.
* [`mirror.kontain.me`](./cmd/mirror), which pulls and caches images from other registries.
* [`flatten.kontain.me`](./cmd/flatten), which pulls and flattens images from other registries,
  so they contain only one layer.
* [`kaniko.kontain.me`](./cmd/kaniko), which builds a GitHub repo using
  [Kaniko](https://github.com/GoogleContainerTools/kaniko).
* [`ko.kontain.me`](./cmd/ko), which builds a Go binary into a container image using
  [`ko`](https://github.com/google/ko).
* [`apko.kontain.me`](./cmd/apko), which builds a minimal base image containing
  APK packages, using [`apko`](https://apko.dev).
* [`buildpack.kontain.me`](./cmd/buildpack), which builds a GitHub repo using [CNCF
  Buildpacks](https://buildpacks.io).
* [`estargz.kontain.me`](./cmd/estargz), which optimizes an image's layers for
  partial image pulls using
  [estargz](https://github.com/containerd/stargz-snapshotter).
* [`wait.kontain.me`](./cmd/wait), which enqueues a background task to serve a
  random image after some amount of time.

This repo also serves [`viz.kontain.me`](./cmd/viz), which visualizes shared
image layers using [Graphviz](https://graphviz.org/).

# Caveats

* The registry does not accept pushes.
* This is a silly hack and probably isn't stable. Don't rely on it for anything
  serious.
* It could probably do a lot of smart things to be a lot faster. 🤷
* Blobs and manifests are cached for 24 hours wherever possible, but will be
  rebuilt from scratch after that time.

# How it works

The service is implemented using [Google Cloud
Run](https://cloud.google.com/run).

When the service receives a request for an image manifest, it parses the
request and generates layers for the requested image, writing the manifest and
blobs to [Google Cloud Storage](https://cloud.google.com/storage/). After it
receives the manifest, `docker pull` fetches the blobs. The app simply
redirects to Cloud Storage to serve manifests and blobs.

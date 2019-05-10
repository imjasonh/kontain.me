**kontain.me** serves Docker container images generated on-demand at the
time they are requested.

# `random.kontain.me`

`docker pull random.kontain.me/random:latest` serves an image containing random
data. By default the image contains one layer containing 10 MB of random bytes.
You can request a specific size and shape of random image. For example,
`kontain.me/random:4x100` generates a random image of 4 layers of 100 random
bytes each.

# `ko.kontain.me`

`docker pull ko.kontain.me/ko/[import path]` serves an image
containing a Go binary fetched using `go get` and built into a
container image using [ko](https://github.com/google/ko).

For example, `docker pull ko.kontain.me/ko/github.com/google/ko/cmd/ko` will
fetch, build and (eventually) serve a Docker image containing `ko` itself.
_Koception!_

# `cnb.kontain.me`

`docker pull cnb.kontain.me/[ghuser]/[ghrepo]:[revision]` serves
an image fetched from source on GitHub and built using [CNCF Buildpacks](https://buildpacks.io)

For example, `docker pull cnb.kontain.me/buildpack/sample-java-app:b032838` fetches, builds
and serves a [sample Java app](https://github.com/buildpack/sample-java-app).

# Caveats

The registry does not accept pushes and does not handle requests for images
by digest. This is a silly hack and probably isn't stable. Don't rely on it for
anything serious. It could probably do a lot of smart things to be a lot
faster.

# How it works

The service is implemented using [Google Cloud
Run](https://cloud.google.com/run), with a [custom domain
mapping](https://cloud.google.com/run/docs/mapping-custom-domains) to
https://kontain.me which provides a managed SSL certificate.

When the service receives a request for an image manifest, it parses the request
and generates layers for the requested image, writing the blobs to [Google Cloud
Storage](https://cloud.google.com/storage/). After it receives the manifest,
`docker pull` fetches the blobs. The app simply redirects to Cloud Storage to
serve the blobs. Blobs are deleted after 10 days.

**kontain.me** serves Docker container images generated on-demand at the
time they are requested.

These include:

* [`random.kontain.me`](./cmd/random), which serves randomly-generated images.
* [`mirror.kontain.me`](./cmd/mirror), which pulls and caches images from other registries.
* [`flatten.kontain.me`](./cmd/flatten), which pulls and flattens images from other registries,
  so they contain only one layer.
* [`ko.kontain.me`](./cmd/ko), which builds a Go binary into a container image using
  [`ko`](https://github.com/google/ko).
* [`buildpack.kontain.me`](./cmd/buildpack), which builds a GitHub repo using [CNCF
  Buildpacks](https://buildpacks.io).

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

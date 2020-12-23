# `buildpack.kontain.me`

`docker pull buildpack.kontain.me/[ghuser]/[ghrepo][/path/to/app]:[revision]`
serves an image fetched from source on GitHub and built using [CNCF
Buildpacks](https://buildpacks.io)

## Examples

Build the latest revision of a repo:

```
docker pull buildpack.kontain.me/googlecloudplatform/buildpack-samples/sample-go
```

Build a specific commit:

```
docker pull buildpack.kontain.me/googlecloudplatform/buildpack-samples/sample-go:ed393d
```

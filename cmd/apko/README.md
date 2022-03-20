# `apko.kontain.me`

`docker pull apko.kontain.me/[package]/[package]/...` serves an image containing the specified apk packages.

## Examples

Build and pull a minimal distroless base image:

```
docker pull apko.kontain.me/alpine-baselayout
```

(PS: you should just use [`ghcr.io/distroless/static`](https://github.com/distroless/static) instead!)

Build and pull an image containing `nginx`:

```
docker pull apko.kontain.me/alpine-baselayout/nginx
```

You can also specify the URL of an image config YAML to fetch, parse and build, if `url` is the first element in the path:

```
docker pull apko.kontain.me/url/raw.githubusercontent.com/chainguard-dev/apko/main/examples/nginx-rootless.yaml
```

# `apko.kontain.me`

`docker pull apko.kontain.me/[package]/[package]/...` serves an image containing the specified apk packages.

## Examples

Build and pull a minimal distroless base image:

```
docker pull apko.kontain.me/wolfi-baselayout
```

(PS: you should just use `cgr.dev/chainguard/static` instead!)

Build and pull an image containing `nginx`:

```
docker pull apko.kontain.me/wolfi-baselayout/nginx
```

In the above examples, packages are provided by the [Wolfi](https://wolfi.dev/os) distro.

You can also specify the URL of an image config YAML to fetch, parse and build, if `url` is the first element in the path:

```
docker pull apko.kontain.me/url/raw.githubusercontent.com/chainguard-dev/apko/main/examples/nginx-rootless.yaml
```

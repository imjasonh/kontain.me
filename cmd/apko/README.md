# `apko.kontain.me`

`docker pull apko.kontain.me/[packages]/[packages]/...` serves an image containing the specified apk packages.

🚨 **This doesn't currently work** 🚨

```
$ crane manifest apko-jdvoki277a-uc.a.run.app/alpine-baselayout/nginx
Error: fetching manifest apko-jdvoki277a-uc.a.run.app/alpine-baselayout/nginx: GET https://apko-jdvoki277a-uc.a.run.app/v2/alpine-baselayout/nginx/manifests/latest: MANIFEST_UNKNOWN: failed to build layer image for "amd64": failed to initialize apk database: exit status 1
```

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
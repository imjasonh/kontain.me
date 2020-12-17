# `mirror.kontain.me`

`docker pull mirror.kontain.me/[image]` will pull the an image (if it can) and
cache the manifest and layers. Subsequent pulls will, if possible, serve from
the cache.

For example, `docker pull mirror.kontain.me/busybox` will pull and cache
`busybox` from Dockerhub. Only public images are supported.

This acts as a simple [registry
mirror](https://docs.docker.com/registry/recipes/mirror/) which can reduce the
number of pulls from the original registry, in case they impose request limits
or exorbitant bandwidth costs or latencies.

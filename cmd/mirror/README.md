# `mirror.kontain.me`

`docker pull mirror.kontain.me/[image]` will pull the an image (if it can) and
cache the manifest and layers. Subsequent pulls will, if possible, serve from
the cache.

Only public images are supported.

This can act as a simple [registry
mirror](https://docs.docker.com/registry/recipes/mirror/) which can reduce the
number of pulls from the original registry, in case they impose request limits
or exorbitant bandwidth costs or latencies.

_This should not be depended on in a production environment. There is no SLO,
ad you should not trust me not to modify the image._

## Examples

Pull a mirrored [`busybox`](https://hub.docker.com/_/busybox) image:

```
docker pull mirror.kontain.me/busybox
```

Or by tag:

```
docker pull mirror.kontain.me/busybox:musl
```

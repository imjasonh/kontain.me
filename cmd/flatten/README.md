# `flatten.kontain.me`

`docker pull flatten.kontain.me/[image]` will pull an image (if it can), then
flatten its layers into a single layer.

Only public images are supported.

_Flattening images obviates image layer caching, so it's often not an
optimization._

## Examples

Pull a flattened [`busybox`](https://hub.docker.com/_/busybox) image:

```
docker pull flatten.kontain.me/busybox
```

Or by tag:

```
docker pull flatten.kontain.me/busybox:musl
```

Flattened images can't be requested by the digest of the unflattened image, because the flattened image digest won't match the original image's digest.

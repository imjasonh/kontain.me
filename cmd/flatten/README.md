# `flatten.kontain.me`

`docker pull flatten.kontain.me/[image]` will pull an image (if it can), then
flatten its layers into a single layer.

For example, `docker pull flatten.kontain.me/busybox` will pull `busybox` from
Dockerhub and flatten it. Only public images are supported.

_Flattening images obviates image layer caching, so it's often not an
optimization._

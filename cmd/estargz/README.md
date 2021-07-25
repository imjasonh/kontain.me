# `estargz.kontain.me`

`docker pull estargz.kontain.me/[image]` will pull an image (if it can), and optimize its layers for partial image pulls using the [estargz](https://github.com/containerd/stargz-snapshotter) format.

Only public repos are supported.

## Examples

You can use this with the containerd stargz-snapshotter to pull image contents on-demand after the registry optimizes the image.
Follow [the Kubernetes setup guide](https://github.com/containerd/stargz-snapshotter#quick-start-with-kubernetes) then create a Pod that pulls an optimized image:

```
kubectl run demo --image=estargz.kontain.me/nginx
```

The first request to pull the image will cause the image to be optimized and cached, and subsequent pulls will be faster.

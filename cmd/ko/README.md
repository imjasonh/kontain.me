# `ko.kontain.me`

`docker pull ko.kontain.me/ko/[import path]` serves an image containing a Go
binary fetched using `go get` and built into a container image using
[ko](https://github.com/google/ko).

For example, `docker pull ko.kontain.me/ko/github.com/google/ko/cmd/ko` will
fetch, build and (eventually) serve a Docker image containing `ko` itself.
_Koception!_

## Examples

Build and pull `ko` itself:

```
docker pull ko.kontain.me/github.com/google/ko/cmd/ko
```

_Koception!_

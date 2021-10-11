# `ko.kontain.me`

`docker pull ko.kontain.me/ko/[import path]` serves an image containing a Go
binary fetched using `go get` and built into a container image using
[ko](https://github.com/google/ko).

## Examples

Build and pull `ko` itself:

```
docker pull ko.kontain.me/github.com/google/ko
```

_Koception!_

Source is pulled from the Go module proxy, and as such, packages must use Go modules.

The `:latest` tag corresponds to the `@latest` version of the Go module, usually the highest semver tagged release.
You can use tags like `:v1.2.3` to pull and build a specific release, or a branch name (e.g., `:main`) to build a branch.

Images are built for all available platforms, depending on their base image.
Manifests are cached for faster rebuilds.

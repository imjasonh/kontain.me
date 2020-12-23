# `kaniko.kontain.me`

`docker pull kaniko.kontain.me/[ghuser]/[ghrepo][/path/to/app]:[revision]`
serves an image fetched from source on GitHub and built using
[Kaniko](https://github.com/GoogleContainerTools/kaniko).

## Caveats

* The `Dockerfile` must be named `Dockerfile`, and must be at the root of the
  path specified in the image name. This path also describes the root of the
  source context.

## Examples

Build the latest revision of a repo:

```
docker pull kaniko.kontain.me/dockersamples/node-bulletin-board/bulletin-board-app
```

Build a specific commit:

```
docker pull kaniko.kontain.me/dockersamples/node-bulletin-board/bulletin-board-app:3f08afd
```

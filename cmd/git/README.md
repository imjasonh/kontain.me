# `git.kontain.me`

`docker pull git.kontain.me/[repo]:[ref]` serves an image containing a shallow
clone of `[repo]` (fetched over HTTPS) checked out at `/git/[name]`, including
its `.git` directory, on top of [`cgr.dev/chainguard/git:latest-dev`].

The image tag `[ref]` is a branch, a tag, or a full commit SHA. It defaults to
`latest`, which means the remote's default branch unless a branch or tag named
`latest` exists. A trailing `.git` on `[repo]` is optional.

[`cgr.dev/chainguard/git:latest-dev`]: https://images.chainguard.dev/directory/image/git/overview

## Examples

Get an image with this repo's default branch checked out at `/git/kontain.me`:

```
docker pull git.kontain.me/github.com/imjasonh/kontain.me
```

```dockerfile
FROM git.kontain.me/github.com/imjasonh/kontain.me AS src
FROM alpine
COPY --from=src /git /git
```

A specific branch, tag, or commit:

```
docker pull git.kontain.me/github.com/google/go-containerregistry:main
docker pull git.kontain.me/github.com/google/go-containerregistry:v0.20.0
docker pull git.kontain.me/github.com/google/go-containerregistry:8a28419ab6b35f06f7148f3b5fc9e75b663f7e2c
```

Since the base image is `cgr.dev/chainguard/git`, the `git` CLI works inside the
checkout:

```
docker run --rm git.kontain.me/github.com/imjasonh/kontain.me git log -1
```

## Caveats

* Branch and tag clones are shallow (`--depth=1`), so history isn't available.
  Pulling by commit SHA does a full clone first.
* Refs with `/` in them (e.g. `feature/foo`) can't be expressed as an OCI tag;
  pull by commit SHA instead.
* The image ships an `/etc/gitconfig` with `safe.directory = *` so `git` works
  in the checkout regardless of which user the container runs as.

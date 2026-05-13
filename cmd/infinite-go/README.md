# `infinite-go.kontain.me`

[![Go Reference](https://pkg.go.dev/badge/infinite-go.kontain.me.svg)](https://pkg.go.dev/infinite-go.kontain.me)

A Go module whose contents change every time you fetch it*. Built on the same
git-HTTP plumbing as [`infinite-git`](../infinite-git), plus a `?go-get=1`
discovery middleware so `go get infinite-go.kontain.me@latest` resolves.

```
$ go mod init example.com/scratch
$ go get -u infinite-go.kontain.me@latest
go: added infinite-go.kontain.me v0.0.0-20260513…
$ go get infinite-go.kontain.me@latest   # again, a few minutes later
go: upgraded infinite-go.kontain.me v0.0.0-20260513… => v0.0.0-20260513…
```

Each fetch returns a fresh pseudo-version whose `pulltime.go` exports a
`PullTime time.Time` set to when the request was served.

\*`proxy.golang.org` caches `@latest` briefly, so two `go get`s back-to-back may return the same
version -- give it a minute and try again, or add `GOPROXY=direct` to skip
the proxy and always see updates:

```
$ export GOPROXY=direct
$ go get -u infinite-go.kontain.me@latest
go: downloading infinite-go.kontain.me v0.0.0-20260513202949-188348487f95
go: upgraded infinite-go.kontain.me v0.0.0-20260513202835-f5b25ff7e160 => v0.0.0-20260513202949-188348487f95
$ go get -u infinite-go.kontain.me@latest
go: downloading infinite-go.kontain.me v0.0.0-20260513202958-a84d6b2d4043
go: upgraded infinite-go.kontain.me v0.0.0-20260513202949-188348487f95 => v0.0.0-20260513202958-a84d6b2d4043
$ go get -u infinite-go.kontain.me@latest
go: downloading infinite-go.kontain.me v0.0.0-20260513203059-a49dcc5379ad
go: upgraded infinite-go.kontain.me v0.0.0-20260513202958-a84d6b2d4043 => v0.0.0-20260513203059-a49dcc5379ad
```

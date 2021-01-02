# `wait.kontain.me`

`docker pull wait.kontain.me/some-unique-string` enqueues a background task to
generate a random image, which will eventually be served.

By default, the task runs after 10 seconds. You can request the delay time (up
to one hour) using the image tag.

After an image is generated for the unique string, it will be served until the
image is evicted in the next 24 hours.

## Examples

Pull a random image available in 10 seconds:

```
docker pull wait.kontain.me/blah-blah
```

Pull a random image available in 30 seconds:

```
docker pull wait.kontain.me/random:30s
```

## Demo

This screencast requests an image that should exist in five seconds, then waits
to see it appear and checks that it's valid.

[![asciicast](https://asciinema.org/a/JUiiq33BaGF3NGx10PP6uvETf.svg)](https://asciinema.org/a/JUiiq33BaGF3NGx10PP6uvETf)

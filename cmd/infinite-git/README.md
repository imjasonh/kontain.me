# `infinite-git.kontain.me`

A Git HTTP server that generates a new commit every time you pull.

```
$ git clone https://infinite-git.kontain.me/ /tmp/infinite-git && cd /tmp/infinite-git
$ cat hello.txt
Pull #0
Timestamp: Initial commit
$ git pull
   df979d4..d483466  main       -> origin/main
$ cat hello.txt
Pull #1
Timestamp: 2026-05-13 …
$ git pull
   d483466..47f0fcb  main       -> origin/main
$ ...
```

The repository lives on Cloud Run's in-memory tmpfs, so it resets when the
instance is replaced. That's part of the gag.

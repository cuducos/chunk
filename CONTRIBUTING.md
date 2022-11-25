# Contributing to Chunk

You might need to [install Staticcheck](https://staticcheck.io/docs/getting-started/#installation) if you don't have it in your system yet.

Then, write and run tests, and use the auto-format as well as the linter tools:

```console
$ gofmt -w ./
$ staticcheck ./...
$ go test -race ./...
```

To make it easy for you, we have pre-commit hooks set up:

```console
$ git config core.hooksPath .githooks
```
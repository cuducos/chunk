# Contributing to Chunk

You might need to [install golangci-lint](https://golangci-lint.run) if you don't have it in your system yet.

Then, write and run tests, and use the auto-format as well as the linter tools:

```console
$ gofmt -w ./
$ golangci-lint run ./...
$ go test -race ./...
```

# Chunk

[![Tests](https://github.com/cuducos/chunk/actions/workflows/tests.yaml/badge.svg)](https://github.com/cuducos/chunk/actions/workflows/tests.yaml)
[![Format](https://github.com/cuducos/chunk/actions/workflows/gofmt.yaml/badge.svg)](https://github.com/cuducos/chunk/actions/workflows/gofmt.yaml)
[![Lint](https://github.com/cuducos/chunk/actions/workflows/golint.yaml/badge.svg)](https://github.com/cuducos/chunk/actions/workflows/golint.yaml)
[![GoDoc](https://godoc.org/github.com/cuducos/chunk?status.svg)](https://godoc.org/github.com/cuducos/chunk)

Chunk is a download tool for slow and unstable servers.

## Usage

### CLI

Install it with `go install github.com/cuducos/chunk` then:

```console
$ chunk <URLs>
```

Use `--help` for detailed instructions.

### API

The [`Download`](https://pkg.go.dev/github.com/cuducos/chunk#Download) method returns a channel with [`DownloadStatus`](https://pkg.go.dev/github.com/cuducos/chunk#DownloadStatus) statuses. This channel is closed once all downloads are finished, but the user is in charge of handling errors.

#### Simplest use case

```go
d := chunk.DefaultDownloader()
ch := d.Dowload(urls)
```

#### Customizing some options

```go
d := chunk.DefaultDownloader()
d.MaxRetries = 42
ch := d.Dowload(urls)
```

#### Customizing everything

```go
d := chunk.Downloader{...}
ch := d.Download(urls)
```

## How?

It uses HTTP range requests, retries per HTTP request (not per file), prevents re-downloading the same content range and supports wait time to give servers time to recover.

### Download using HTTP range requests

In order to complete downloads from slow and unstable servers, the download should be done in “chunks” using [HTTP range requests](https://developer.mozilla.org/en-US/docs/Web/HTTP/Range_requests). This does not rely on long-standing HTTP connections, and it makes it predictable the idea of how long is too long for a non-response.

### Retries by chunk, not by file

In order to be quicker and avoid rework, the primary way to handle failure is to retry that “chunk” (content range), not the whole file.

### Control of which chunks are already downloaded

In order to avoid re-starting from the beginning in case of non-handled errors, `chunk` knows which ranges from each file were already downloaded; so, when restarted, it only downloads what is really needed to complete the downloads.

### Detect server failures and give it a break

In order to avoid unnecessary stress on the server, `chunk` relies not only on HTTP responses but also on other signs that the connection is stale and can recover from that and give the server some time to recover from stress.

## Why?

The idea of the project emerged as it was difficult for [Minha Receita](https://github.com/cuducos/minha-receita) to handle the download of [37 files that adds up to just approx. 5Gb](https://www.gov.br/receitafederal/pt-br/assuntos/orientacao-tributaria/cadastros/consultas/dados-publicos-cnpj). Most of the download solutions out there (e.g. [`got`](https://github.com/melbahja/got)) seem to be prepared for downloading large files, not for downloading from slow and unstable servers — which is the case at hand.


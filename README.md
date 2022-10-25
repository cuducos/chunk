# Chunk

`chunk` is a sort of download manager written in pure Go. The idea of the project emerged as it was difficult for [Minha Receita](https://github.com/cuducos/minha-receita) to handle the download of [37 files that adds up to just approx. 5Gb](https://www.gov.br/receitafederal/pt-br/assuntos/orientacao-tributaria/cadastros/consultas/dados-publicos-cnpj). Most of the download solutions out there (e.g. [`got`](https://github.com/melbahja/got)) seem to be prepared for downloading large files, not for downloading from slow and unstable servers — which is the case at hand.

## Main fetaures

### Download using HTTP range requests

In order to complete downloads from slow and unstable servers, the download should be done in “chunks” using [HTTP range requests](https://developer.mozilla.org/en-US/docs/Web/HTTP/Range_requests). This does not rely on long-standing HTTP connections, and it makes it predictable the idea of how long is too long for a non-response.

### Retries by chunk, not by file

In order to be quicker and avoid rework, the primary way to handle failure is to retry that “chunk” (that bytes range), not the whole file.

### Control of which chunks are already downloaded

In order to avoid re-starting from the beginning in case of non-handled errors, `chunk` knows which ranges from each file were already downloaded; so, when restarted, it only downloads what is really needed to complete the downloads.

### Detect server failures and give it a break

In order to avoid unnecessary stress on the server, `chunk` relies not only on HTTP responses but also on other signs that the connection is stale and can:

1. recover from that and
2. give the server some time to recover from stress.

## Tech design

### Input

* List of URLs
* Directory where to save the files
* Configuration (they can have defaults and be optional; customizing them can be a stretch goal):
  * Chunck download attempt timeout
  * Maximum parallel connection to each server
  * Max retries per chunk (must have an option to unlimited)
  * Range maximum size (chunk size)
  * Time to wait on server failure

### Prepare downloads

For each URL of the list (this can be done in parallel):

* Make sure the server accepts HTTP range requests (stretch goal)
  * Can fail if it doesn't
  * Or can default to regular HTTP request to download
* Find out the file total size
* Determine all the chunks to be downloaded (each start and end bytes)
* Read or create a temporary control of chunks downloaded and pending chunks
* Enqueue all the pending chunks

With all this information, show a progress bar with the total work remaining.

### Download

* Set a timeout
* Start the HTTP range request
* In case of failure or timeout, re-queue this chunk
* In case of success, send the chunk contents to a `results` channel

### Writing files

* Read the bytes from the `results` channel
* Write to the file on disk
* Update a progress bar to give the user an idea about the status of the downloads

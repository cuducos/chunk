package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/avast/retry-go"
)

const (
	DefaultTimeoutPerChunk               = 90 * time.Second
	DefaultMaxParallelDownloadsPerServer = 8
	DefaultMaxRetriesPerChunk            = 5
	DefaultChunkSize                     = 8192
	DefaultWaitBetweenRetries            = 0 * time.Minute
)

// DownloadStatus is the data propagated via the channel sent back to the user
// and it contains information about the download from each URL.
type DownloadStatus struct {
	// URL this status refers to
	URL string

	// DownloadedFilePath in the user local system
	DownloadedFilePath string

	// FileSizeBytes is the total size of the file as informed by the server
	FileSizeBytes uint64

	// DownloadedFileBytes already downloaded from this URL
	DownloadedFileBytes uint64

	// Any non-recoerable error captured during the download (this means that
	// some errors are ignored the download is retried instead of propagating
	// the error).
	Error error
}

// IsFinished informs the user whether a download is done (successfully or
// with error).
func (s *DownloadStatus) IsFinished() bool {
	return s.Error != nil || s.DownloadedFileBytes == s.FileSizeBytes
}

// Downloader can be configured by the user before starting the download using
// the following fields. This configurations impacts how the download will be
// handled, including retries, amoutn of requets, and size of each request, for
// example.
type Downloader struct {
	// Client is the HTTP client used for every request needed to download all
	// the files.
	client *http.Client

	// TimeoutPerChunk is the timeout for the download of each chunk from each
	// URL. A chunk is a part of a file requested using the content range HTTP
	// header. Thus, this timeout is not the timeout for the each file or for
	// the the download of every file).
	TimeoutPerChunk time.Duration

	// MaxParallelDownloadsPerServer controls how many requests are sent in
	// parallel to the same server. If all the URLs are from the same server
	// this is the total of parallel requests. If the user is downloading files
	// from different servers (including different subdomains), this limit is
	// applied to each server idependently.
	MaxParallelDownloadsPerServer uint

	// MaxRetriesPerChunk is the maximum amount of retries for each HTTP request
	// using the content range header that fails.
	MaxRetriesPerChunk uint

	// ChunkSize is the maximum size of each HTTP request done using the
	// content range header. There is no way to specify how many chunks a
	// download will need, the focus is on slicing it in smaller chunks so slow
	// and unstable servers can respond before dropping it.
	ChunkSize uint64

	// WaitBetweenRetries is an optional pause before retrying an HTTP request
	// that has failed.
	WaitBetweenRetries time.Duration
}

func (d *Downloader) downloadFileWithContext(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating the request for %s: %w", u, err)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending a get http request to %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("got http response %s from %s: %w", resp.Status, u, err)
	}
	var b bytes.Buffer
	_, err = b.ReadFrom(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading body from %s: %w", u, err)
	}
	return b.Bytes(), nil
}

func (d *Downloader) downloadFileWithTimeout(userCtx context.Context, u string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(userCtx, d.TimeoutPerChunk) // need to propagate context, which might contain app-specific data.
	defer cancel()
	ch := make(chan []byte)
	errs := make(chan error)
	go func() {
		b, err := d.downloadFileWithContext(ctx, u)
		if err != nil {
			errs <- err
			return
		}
		ch <- b
	}()
	select {
	case <-userCtx.Done():
		cancel()
		return nil, userCtx.Err()
	case <-ctx.Done():
		return nil, fmt.Errorf("request to %s ended due to timeout: %w", u, ctx.Err())
	case err := <-errs:
		return nil, fmt.Errorf("request to %s failed: %w", u, err)
	case b := <-ch:
		return b, nil
	}
}

func (d *Downloader) getContentSizeHeader(ctx context.Context, u string) (uint64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
	if err != nil {
		return 0, fmt.Errorf("error creating the request for %s: %w", u, err)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("error sending a get http request to %s: %w", u, err)
	}
	defer resp.Body.Close()
	return uint64(req.ContentLength), nil
}

func (d *Downloader) downloadFile(ctx context.Context, u string) ([]byte, error) {
	ch := make(chan []byte, 1)
	defer close(ch)
	err := retry.Do(
		func() error {
			b, err := d.downloadFileWithTimeout(ctx, u)
			if err != nil {
				return err
			}
			ch <- b
			return nil
		},
		retry.Attempts(d.MaxRetriesPerChunk),
		retry.MaxDelay(d.WaitBetweenRetries),
	)
	if err != nil {
		return nil, fmt.Errorf("error downloading %s: %w", u, err)
	}
	b := <-ch
	return b, nil
}

// DownloadWithContext is a version of Download that takes a context. The
// context can be used to stop all downloads in progress.
func (d *Downloader) DownloadWithContext(ctx context.Context, urls ...string) <-chan DownloadStatus {
	if d.client == nil {
		d.client = &http.Client{Timeout: d.TimeoutPerChunk}
	}
	ch := make(chan DownloadStatus)
	var wg sync.WaitGroup
	for _, u := range urls {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			s := DownloadStatus{URL: u}
			defer func() { ch <- s }()
			s.DownloadedFilePath = filepath.Join(os.TempDir(), filepath.Base(u))
			b, err := d.downloadFile(ctx, u)
			if err != nil {
				s.Error = err
				return
			}
			if err := os.WriteFile(s.DownloadedFilePath, b, 0655); err != nil {
				s.Error = err
				return
			}
			ds, err := d.getContentSizeHeader(ctx, u)
			if err != nil {
				s.Error = err
				return
			}
			s.DownloadedFileBytes = uint64(len(b))
			if ds == 0 {
				s.FileSizeBytes = uint64(len(b))
			} else {
				s.FileSizeBytes = ds
			}
		}(u)
	}
	go func() {
		wg.Wait()
		close(ch)
	}()
	return ch
}

// Download from all URLs slicing each in a series of chunks, of small HTTP
// requests using the content range header.
func (d *Downloader) Download(urls ...string) <-chan DownloadStatus {
	return d.DownloadWithContext(context.Background(), urls...)
}

// NewDownloader creates a downloader with the defalt configuration. Check
// the constants in this package for their values.
func DefaultDownloader() *Downloader {
	return &Downloader{
		TimeoutPerChunk:               DefaultTimeoutPerChunk,
		MaxParallelDownloadsPerServer: DefaultMaxParallelDownloadsPerServer,
		MaxRetriesPerChunk:            DefaultMaxRetriesPerChunk,
		ChunkSize:                     DefaultChunkSize,
		WaitBetweenRetries:            DefaultWaitBetweenRetries,
	}
}

func main() {
	d := DefaultDownloader()
	for s := range d.Download(os.Args[1:len(os.Args)]...) {
		if s.Error != nil {
			log.Fatal(s.Error)
		}
		if s.IsFinished() {
			log.Printf("Downloaded %s (%d bytes) to %s (%d bytes).", s.URL, s.FileSizeBytes, s.DownloadedFilePath, s.DownloadedFileBytes)
		}
	}
	log.Println("All download(s) finished successfully.")
}

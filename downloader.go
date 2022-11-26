package chunk

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/avast/retry-go"
)

const (
	DefaultTimeout              = 90 * time.Second
	DefaultConcurrencyPerServer = 8
	DefaultMaxRetries           = 5
	DefaultChunkSize            = 8192
	DefaultWaitRetry            = 1 * time.Second
	DefaultContinueDownloads    = false
)

// DownloadStatus is the data propagated via the channel sent back to the user
// and it contains information about the download from each URL.
type DownloadStatus struct {
	// URL this status refers to
	URL string

	// DownloadedFilePath in the user local system
	DownloadedFilePath string

	// FileSizeBytes is the total size of the file as informed by the server
	FileSizeBytes int64

	// DownloadedFileBytes already downloaded from this URL
	DownloadedFileBytes int64

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
	// OutputDir is where the downloaded files will be saved.  If not set,
	// defaults to the current working directory.
	OutputDir string

	// client is the HTTP client used for every request needed to download all
	// the files.
	client *http.Client

	// TimeoutPerChunk is the timeout for the download of each chunk from each
	// URL. A chunk is a part of a file requested using the content range HTTP
	// header. Thus, this timeout is not the timeout for the each file or for
	// the the download of every file).
	Timeout time.Duration

	// MaxParallelDownloadsPerServer controls the max number of concurrent
	// connections opened to the same server. If all the URLs are from the same
	// server this is the total of concurrent connections. If the user is downloading
	// files from different servers, this limit is applied to each server idependently.
	ConcurrencyPerServer int

	// MaxRetriesPerChunk is the maximum amount of retries for each HTTP request
	// using the content range header that fails.
	MaxRetries uint

	// ChunkSize is the maximum size of each HTTP request done using the
	// content range header. There is no way to specify how many chunks a
	// download will need, the focus is on slicing it in smaller chunks so slow
	// and unstable servers can respond before dropping it.
	ChunkSize int64

	// WaitBetweenRetries is an optional pause before retrying an HTTP request
	// that has failed.
	WaitRetry time.Duration

	// ContinueDownloads controls whether or not to continue the download of
	// previous download attempts, skipping chunks alreadt downloaded.
	ContinueDownloads bool
}

type chunk struct{ start, end int64 }

func (c chunk) size() int64         { return (c.end + 1) - c.start }
func (c chunk) rangeHeader() string { return fmt.Sprintf("bytes=%d-%d", c.start, c.end) }

func (d *Downloader) downloadChunkWithContext(ctx context.Context, u string, c chunk) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating the request for %s: %w", u, err)
	}
	req.Header.Set("Range", c.rangeHeader())
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending a get http request to %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("got http response %s from %s: %w", resp.Status, u, err)
	}
	var b bytes.Buffer
	_, err = b.ReadFrom(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading body from %s: %w", u, err)
	}
	return b.Bytes(), nil
}

func (d *Downloader) downloadChunkWithTimeout(userCtx context.Context, u string, c chunk) ([]byte, error) {
	ctx, cancel := context.WithTimeout(userCtx, d.Timeout) // need to propagate context, which might contain app-specific data.
	defer cancel()
	ch := make(chan []byte)
	errs := make(chan error)
	go func() {
		b, err := d.downloadChunkWithContext(ctx, u, c)
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

func (d *Downloader) getDownloadSize(ctx context.Context, u string) (int64, error) {
	ch := make(chan *http.Response, 1)
	defer close(ch)
	err := retry.Do(
		func() error {
			req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
			if err != nil {
				return fmt.Errorf("creating the request for %s: %w", u, err)
			}
			resp, err := d.client.Do(req)
			if err != nil {
				return fmt.Errorf("dispatching the request for %s: %w", u, err)
			}
			if resp.StatusCode != 200 {
				return fmt.Errorf("got unexpected http response status for %s: %s", u, resp.Status)
			}
			ch <- resp
			return nil
		},
		retry.Attempts(d.MaxRetries),
		retry.MaxDelay(d.WaitRetry),
	)
	if err != nil {
		return 0, fmt.Errorf("error sending get http request to %s: %w", u, err)
	}
	resp := <-ch
	defer resp.Body.Close()
	if resp.ContentLength <= 0 {
		var s int64
		r := strings.TrimSpace(resp.Header.Get("Content-Range"))
		if r == "" {
			return 0, fmt.Errorf("could not get content length for %s", u)
		}
		p := strings.Split(r, "/")
		fmt.Sscan(p[len(p)-1], &s)
		return s, nil
	}
	return resp.ContentLength, nil
}

func (d *Downloader) downloadChunk(ctx context.Context, u string, c chunk) ([]byte, error) {
	ch := make(chan []byte, 1)
	defer close(ch)
	err := retry.Do(
		func() error {
			b, err := d.downloadChunkWithTimeout(ctx, u, c)
			if err != nil {
				return err
			}
			ch <- b
			return nil
		},
		retry.Attempts(d.MaxRetries),
		retry.MaxDelay(d.WaitRetry),
	)
	if err != nil {
		return nil, fmt.Errorf("error downloading %s: %w", u, err)
	}
	b := <-ch
	return b, nil
}

func (d *Downloader) chunks(t int64) []chunk {
	var start int64
	last := t - 1
	var c []chunk
	for {
		end := start + d.ChunkSize - 1
		if end > last {
			end = last
		}
		c = append(c, chunk{start, end})
		if end == last {
			break
		}
		start = end + 1
	}
	return c
}

func (d *Downloader) prepareAndStartDownload(ctx context.Context, url string, ch chan<- DownloadStatus) {
	path := filepath.Join(d.OutputDir, filepath.Base(url))
	s := DownloadStatus{URL: url, DownloadedFilePath: path}
	t, err := d.getDownloadSize(ctx, url)
	if err != nil {
		s.Error = fmt.Errorf("error getting file size: %w", err)
		ch <- s
		return
	}
	s.FileSizeBytes = t
	ch <- s // send total file size to the user
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		s.Error = fmt.Errorf("error creating %s: %w", path, err)
		ch <- s
		return
	}
	chunks := d.chunks(t)
	p, err := newProgress(s.DownloadedFilePath, s.URL, d.ChunkSize, len(chunks), d.ContinueDownloads)
	if err != nil {
		s.Error = fmt.Errorf("could not creat a progress file: %w", err)
		ch <- s
		return
	}
	var urlDownload sync.WaitGroup
	defer func() {
		urlDownload.Wait()
		p.close()
		f.Close()
	}()
	if err := f.Truncate(int64(t)); err != nil {
		s.Error = fmt.Errorf("error truncating %s to %d: %w", path, t, err)
		ch <- s
		return
	}
	for idx, c := range chunks {
		pending, err := p.shouldDownload(idx)
		if err != nil {
			s.Error = fmt.Errorf("could not determine whether chunk #%d is pending: %w", idx+1, err)
			ch <- s
			return
		}
		if !pending {
			continue
		}
		urlDownload.Add(1)
		go func(c chunk, idx int, s DownloadStatus) {
			defer urlDownload.Done()
			b, err := d.downloadChunk(ctx, url, c)
			if err != nil {
				s.Error = fmt.Errorf("error downloadinf chunk #%d: %w", idx+1, err)
				ch <- s
				return
			}
			n, err := f.WriteAt(b, c.start)
			if err != nil {
				s.Error = fmt.Errorf("error writing to %s: %w", path, err)
				ch <- s
				return
			}
			if err := p.done(idx); err != nil {
				s.Error = fmt.Errorf("error checking chunk #%d as done: %w", idx+1, err)
				ch <- s
				return
			}
			s.DownloadedFileBytes += int64(n)
			ch <- s
		}(c, idx, s)
	}
}

// DownloadWithContext is a version of Download that takes a context. The
// context can be used to stop all downloads in progress.
func (d *Downloader) DownloadWithContext(ctx context.Context, urls ...string) <-chan DownloadStatus {
	if d.client == nil {
		d.client = newClient(d.ConcurrencyPerServer, d.Timeout)
	}
	ch := make(chan DownloadStatus, 2*len(urls)) // the first status will be the total file size (and or an error creating/trucating the file).
	var wg sync.WaitGroup                        // this wait group is used to wait for all chunks (from all downloads) to finish.
	for _, u := range urls {
		wg.Add(1)
		go func(u string) {
			d.prepareAndStartDownload(ctx, u, ch)
			wg.Done()
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
	dir, err := os.Getwd()
	if err != nil {
		dir = ""
	}
	return &Downloader{
		OutputDir:            dir,
		Timeout:              DefaultTimeout,
		ConcurrencyPerServer: DefaultConcurrencyPerServer,
		MaxRetries:           DefaultMaxRetries,
		ChunkSize:            DefaultChunkSize,
		WaitRetry:            DefaultWaitRetry,
		client:               newClient(DefaultMaxRetries, DefaultTimeout),
	}
}

func newClient(maxParallelDownloadsPerServer int, timeoutPerChunk time.Duration) *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxConnsPerHost = maxParallelDownloadsPerServer
	t.MaxIdleConnsPerHost = maxParallelDownloadsPerServer
	return &http.Client{
		Timeout:   timeoutPerChunk,
		Transport: t,
	}
}

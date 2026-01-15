package chunk

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var timeout = 250 * time.Millisecond

func TestDownload_HTTPFailure(t *testing.T) {
	t.Parallel()
	s := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.Header().Add("Content-Length", "2")
				return
			}
			w.WriteHeader(http.StatusBadRequest)
		},
	))
	defer s.Close()
	d := Downloader{
		OutputDir:            t.TempDir(),
		Timeout:              timeout,
		MaxRetries:           4,
		ConcurrencyPerServer: 1,
		ChunkSize:            1024,
		WaitRetry:            1 * time.Millisecond,
	}
	ch := d.Download(s.URL)
	<-ch // discard the first got (just the file size)
	got := <-ch
	if got.Error == nil {
		t.Error("expected an error, but got nil")
	}
	if _, ok := <-ch; ok {
		t.Error("expected channel closed, but did not get it")
	}
}

func TestDownload_ServerTimeout(t *testing.T) {
	t.Parallel()
	s := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.Header().Add("Content-Length", "2")
				return
			}
			time.Sleep(10 * timeout) // server sleeps, causing client timeout
		},
	))
	defer s.Close()
	d := Downloader{
		OutputDir:            t.TempDir(),
		Timeout:              timeout,
		MaxRetries:           4,
		ConcurrencyPerServer: 1,
		ChunkSize:            1024,
		WaitRetry:            1 * time.Millisecond,
	}
	// context with timeout to eventually terminate the download.
	ctx, cancel := context.WithTimeout(context.Background(), 3*timeout)
	defer cancel()
	ch := d.DownloadWithContext(ctx, s.URL)
	<-ch // discard the first status (file size)
	got := <-ch
	if got.Error == nil {
		t.Error("expected an error due to context timeout, but got nil")
	}
	if _, ok := <-ch; ok {
		t.Error("expected channel closed, but did not get it")
	}
}

func TestDownload_DefaultDownloader(t *testing.T) {
	t.Parallel()
	s := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.Header().Add("Content-Length", "2")
				return
			}
			if _, err := io.WriteString(w, "42"); err != nil {
				t.Errorf("failed to write response: %v", err)
			}
		},
	))
	defer s.Close()

	d := DefaultDownloader()
	d.OutputDir = t.TempDir()
	ch := d.Download(s.URL + "/My%20File.txt")
	<-ch // discard the first status (just the file size)
	got := <-ch
	defer func() {
		if err := os.Remove(got.DownloadedFilePath); err != nil {
			t.Errorf("failed to remove test file: %v", err)
		}
	}()

	if got.Error != nil {
		t.Errorf("invalid error. want:nil got:%q", got.Error)
	}
	if got.URL != s.URL+"/My%20File.txt" {
		t.Errorf("invalid URL. want:%s got:%s", s.URL+"/My%20File.txt", got.URL)
	}
	if got.DownloadedFileBytes != 2 {
		t.Errorf("invalid DownloadedFileBytes. want:2 got:%d", got.DownloadedFileBytes)
	}
	if !strings.HasSuffix(got.DownloadedFilePath, "My File.txt") {
		t.Errorf("expected DownloadedFilePath to enfd with My File.txt, got %s", got.DownloadedFilePath)
	}
	if got.FileSizeBytes != 2 {
		t.Errorf("invalid FileSizeBytes. want:2 got:%d", got.FileSizeBytes)
	}
	b, err := os.ReadFile(got.DownloadedFilePath)
	if err != nil {
		t.Errorf("error reading downloaded file (%s): %q", got.DownloadedFilePath, err)
	}
	if string(b) != "42" {
		t.Errorf("invalid downloaded file content. want:42 got:%s", string(b))
	}
	if _, ok := <-ch; ok {
		t.Error("expected channel closed, but did not get it")
	}
}

func TestDownload_ZIPArchive(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	pth := filepath.Join(tmp, "archive.zip")
	expected := make([]byte, 2<<20)
	for i := range 2 << 20 {
		expected[i] = byte(97 + rand.Intn(122-97))
	}

	// create a zip archive
	func() {
		z, err := os.Create(pth)
		if err != nil {
			t.Errorf("expected no error creating zip archive, got %s", err)
		}
		defer func() {
			if err := z.Close(); err != nil {
				t.Errorf("failed to close zip file: %v", err)
			}
		}()
		w := zip.NewWriter(z)
		f, err := w.Create("file.txt")
		if err != nil {
			t.Errorf("expected no error creating archived file, got %s", err)
		}
		defer func() {
			if err := w.Close(); err != nil {
				t.Errorf("failed to close zip writer: %v", err)
			}
		}()
		if _, err := f.Write(expected); err != nil {
			t.Errorf("expected no error writing to archived file, got %s", err)
		}
	}()

	// create a server to serve the zip archive
	s := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			http.ServeFile(w, r, pth)
		},
	))
	defer s.Close()

	// download
	var got string
	defer func() {
		if got != "" {
			if err := os.Remove(got); err != nil {
				t.Errorf("failed to remove test file: %v", err)
			}
		}
	}()
	d := DefaultDownloader()
	d.OutputDir = t.TempDir()
	for g := range d.Download(s.URL + "/archive.zip") {
		got = g.DownloadedFilePath
		if g.Error != nil {
			t.Errorf("expected no error during the download of the zip archive, got %s", g.Error)
		}
	}

	// unarchive and check contents
	a, err := zip.OpenReader(got)
	if err != nil {
		t.Errorf("expected no error opening downloaded zip archive %s, got %s", got, err)
	}
	defer func() {
		if err := a.Close(); err != nil {
			t.Errorf("failed to close zip reader: %v", err)
		}
	}()
	r, err := a.Open("file.txt")
	if err != nil {
		t.Errorf("expected no error reading downloaded zip archive, got %s", err)
	}
	defer func() {
		if err := r.Close(); err != nil {
			t.Errorf("failed to close zip file reader: %v", err)
		}
	}()
	var b bytes.Buffer
	if _, err := io.Copy(&b, r); err != nil {
		t.Errorf("expected no error reading archived file, got %s", err)
	}
	if !bytes.Equal(expected, b.Bytes()) {
		t.Error("archived contents differ from expected") // not printing because it's a lot of data
	}
}

func TestDownload_Retry(t *testing.T) {
	t.Parallel()
	var done atomic.Bool
	s := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.Header().Add("Content-Length", "2")
				return
			}
			if done.CompareAndSwap(false, true) {
				w.WriteHeader(http.StatusTeapot)
				return
			}
			if _, err := io.WriteString(w, "42"); err != nil {
				t.Errorf("failed to write response: %v", err)
			}
		},
	))
	defer s.Close()

	d := Downloader{
		OutputDir:            t.TempDir(),
		Timeout:              timeout,
		MaxRetries:           4,
		ConcurrencyPerServer: 1,
		ChunkSize:            1024,
		WaitRetry:            1 * time.Millisecond,
	}
	ch := d.Download(s.URL)
	<-ch // discard the first status (just the file size)
	got := <-ch
	if got.Error != nil {
		t.Errorf("invalid error. want:nil got:%q", got.Error)
	}
	if !done.Load() {
		t.Error("expected server to be done, it is not")
	}
	if got.URL != s.URL {
		t.Errorf("invalid URL. want:%s got:%s", s.URL, got.URL)
	}
	if got.DownloadedFileBytes != 2 {
		t.Errorf("invalid DownloadedFileBytes. want:2 got:%d", got.DownloadedFileBytes)
	}
	if got.FileSizeBytes != 2 {
		t.Errorf("invalid FileSizeBytes. want:2 got:%d", got.FileSizeBytes)
	}
	b, err := os.ReadFile(got.DownloadedFilePath)
	if err != nil {
		t.Errorf("error reading downloaded file (%s): %q", got.DownloadedFilePath, err)
	}
	if string(b) != "42" {
		t.Errorf("invalid downloaded file content. want:42 got:%s", string(b))
	}
	if _, ok := <-ch; ok {
		t.Error("expected channel closed, but did not get it")
	}
}

func TestDownload_ReportPreviouslyDownloadedBytes(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	pdir := t.TempDir()

	// server that serves first chunk but always fails on second chunk
	s := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.Header().Add("Content-Length", "4")
				return
			}
			rangeHeader := r.Header.Get("Range")
			if strings.HasPrefix(rangeHeader, "bytes=0-") {
				w.WriteHeader(http.StatusPartialContent)
				if _, err := io.WriteString(w, "42"); err != nil {
					t.Errorf("failed to write response: %v", err)
				}
				return
			}
			w.WriteHeader(http.StatusTeapot)
		},
	))
	defer s.Close()

	// first download attempt: first chunk succeeds, second chunk always fails
	d := Downloader{
		OutputDir:            tmp,
		ProgressDir:          pdir,
		Timeout:              timeout,
		MaxRetries:           1,
		ConcurrencyPerServer: 1,
		ChunkSize:            2,
	}
	ch := d.Download(s.URL)
	var first DownloadStatus
	for s := range ch {
		first = s
	}

	// second download attempt: should report the previously downloaded bytes
	d = Downloader{
		OutputDir:            tmp,
		ProgressDir:          pdir,
		Timeout:              timeout,
		MaxRetries:           1,
		ConcurrencyPerServer: 1,
		ChunkSize:            2,
	}
	ch = d.Download(s.URL)
	var second DownloadStatus
	for s := range ch {
		second = s
	}
	if first.DownloadedFileBytes != second.DownloadedFileBytes {
		t.Errorf("expected the same number of downloaded bytes, got %d and %d", first.DownloadedFileBytes, second.DownloadedFileBytes)
	}
}

func TestDownloadWithContext_ErrorUserTimeout(t *testing.T) {
	t.Parallel()
	usrTimeout := 250 * time.Millisecond // smaller than overall timeout
	chunkTimeout := 10 * usrTimeout
	s := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.Header().Add("Content-Length", "2")
				return
			}
			time.Sleep(2 * usrTimeout) // greater than the user timeout, but shorter than the timeout per chunk.
		},
	))
	defer s.Close()
	d := Downloader{
		OutputDir:            t.TempDir(),
		Timeout:              chunkTimeout,
		MaxRetries:           4,
		ConcurrencyPerServer: 1,
		ChunkSize:            1024,
		WaitRetry:            0 * time.Second,
	}
	userCtx, cancFunc := context.WithTimeout(context.Background(), usrTimeout)
	defer cancFunc()

	ch := d.DownloadWithContext(userCtx, s.URL)
	<-ch // discard the first got (just the file size)
	got := <-ch
	if got.Error == nil {
		t.Error("expected an error, but got nil")
	}
	if _, ok := <-ch; ok {
		t.Error("expected channel closed, but did not get it")
	}
}

func TestDownload_Chunks(t *testing.T) {
	t.Parallel()
	d := DefaultDownloader()
	d.ChunkSize = 5
	got := d.chunks(12)
	chunks := []chunk{{0, 4}, {5, 9}, {10, 11}}
	sizes := []int64{5, 5, 2}
	headers := []string{"bytes=0-4", "bytes=5-9", "bytes=10-11"}
	if len(got) != len(chunks) {
		t.Errorf("expected %d chunks, got %d", len(chunks), len(got))
	}
	for i := range got {
		if got[i].start != chunks[i].start {
			t.Errorf("expected chunk #%d to start at %d, got %d", i+1, chunks[i].start, got[i].start)
		}
		if got[i].end != chunks[i].end {
			t.Errorf("expected chunk #%d to end at %d, got %d", i+1, chunks[i].end, got[i].end)
		}
		if got[i].size() != sizes[i] {
			t.Errorf("expected chunk #%d to have size %d, got %d", i+1, sizes[i], got[i].size())
		}
		if got[i].rangeHeader() != headers[i] {
			t.Errorf("expected chunk #%d header to be %s, got %s", i+1, headers[i], got[i].rangeHeader())
		}
	}
}

func TestGetDownload_WithUserAgent(t *testing.T) {
	t.Parallel()
	ua := "Answer/42.0"
	s := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.UserAgent() != ua {
				t.Errorf("expected user-agent to be %s, got %s", ua, r.UserAgent())
			}
		},
	))
	defer s.Close()
	d := DefaultDownloader()
	d.UserAgent = ua
	<-d.Download(s.URL)
}

func TestGetDownloadSize_ContentLength(t *testing.T) {
	t.Parallel()
	s := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if _, err := io.WriteString(w, "Test"); err != nil {
				t.Errorf("failed to write response: %v", err)
			}
		},
	))
	defer s.Close()

	d := DefaultDownloader()
	got, err := d.getDownloadSize(context.Background(), s.URL)

	if err != nil {
		t.Errorf("expected no error getting the file size, got %s", err)
	}
	if got != 4 {
		t.Errorf("invalid size, expected 4, got: %d", got)
	}
}

func TestGetDownloadSize_WithRetry(t *testing.T) {
	t.Parallel()
	attempts := int32(0)
	s := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if atomic.CompareAndSwapInt32(&attempts, 0, 1) {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			if _, err := io.WriteString(w, "Test"); err != nil {
				t.Errorf("failed to write response: %v", err)
			}
		},
	))
	defer s.Close()

	d := DefaultDownloader()
	got, err := d.getDownloadSize(context.Background(), s.URL)

	if err != nil {
		t.Errorf("expected no error getting the file size, got %s", err)
	}
	if got != 4 {
		t.Errorf("invalid size, expected 4, got: %d", got)
	}
}

func TestGetDownloadSize_ContentRange(t *testing.T) {
	t.Parallel()
	s := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Range", "bytes 1-10/123")
			if _, err := io.WriteString(w, ""); err != nil {
				t.Errorf("failed to write response: %v", err)
			}
		},
	))
	defer s.Close()

	d := DefaultDownloader()
	got, err := d.getDownloadSize(context.Background(), s.URL)

	if err != nil {
		t.Errorf("expected no error getting the file size, got %s", err)
	}
	if got != 123 {
		t.Errorf("invalid size, expected 123, got: %d", got)
	}
}

func TestGetDownloadSize_ErrorInvalidURL(t *testing.T) {
	t.Parallel()
	d := Downloader{
		MaxRetries: 1,
		WaitRetry:  0,
		Client:     http.DefaultClient,
	}
	got, err := d.getDownloadSize(context.Background(), "test")

	if err == nil {
		t.Errorf("expected an error, got nil")
	}
	if got != 0 {
		t.Errorf("invalid size, expected 0, got: %d", got)
	}
}

func TestGetDownloadSize_NoContent(t *testing.T) {
	t.Parallel()
	s := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if _, err := io.WriteString(w, ""); err != nil {
				t.Errorf("failed to write response: %v", err)
			}
		},
	))
	defer s.Close()

	d := DefaultDownloader()
	if _, err := d.getDownloadSize(context.Background(), s.URL); err == nil {
		t.Error("expected error getting the file size, got nil")
	}
}

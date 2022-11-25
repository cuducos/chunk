package chunk

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
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

func TestDownload_Error(t *testing.T) {
	timeout := 250 * time.Millisecond
	for _, tc := range []struct {
		desc string
		proc func(w http.ResponseWriter)
	}{
		{"failure", func(w http.ResponseWriter) { w.WriteHeader(http.StatusBadRequest) }},
		{"timeout", func(w http.ResponseWriter) { time.Sleep(10 * timeout) }},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodHead {
						w.Header().Add("Content-Length", "2")
						return
					}
					tc.proc(w)
				},
			))
			defer s.Close()
			d := Downloader{
				OutputDir:            t.TempDir(),
				Timeout:              timeout,
				MaxRetries:           4,
				ConcurrencyPerServer: 1,
				ChunkSize:            1024,
				WaitRetry:            0 * time.Second,
			}
			ch := d.Download(s.URL)
			<-ch // discard the first got (just the file size)
			got := <-ch
			if got.Error == nil {
				t.Error("expected an error, but got nil")
			}
			if !strings.Contains(got.Error.Error(), "#4") {
				t.Error("expected #4 (configured number of retries), but did not get it")
			}
			if _, ok := <-ch; ok {
				t.Error("expected channel closed, but did not get it")
			}
		})
	}
}

func TestDownload_OkWithDefaultDownloader(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.Header().Add("Content-Length", "2")
				return
			}
			fmt.Fprint(w, "42")
		},
	))
	defer s.Close()

	d := DefaultDownloader()
	d.OutputDir = t.TempDir()
	ch := d.Download(s.URL)
	<-ch // discard the first status (just the file size)
	got := <-ch
	defer os.Remove(got.DownloadedFilePath)

	if got.Error != nil {
		t.Errorf("invalid error. want:nil got:%q", got.Error)
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

func TestDownload_ZIPArchive(t *testing.T) {
	tmp := t.TempDir()
	pth := filepath.Join(tmp, "archive.zip")
	expected := make([]byte, 1_000_000)
	for i := 0; i < 1_000_000; i++ {
		expected[i] = byte(97 + rand.Intn(122-97))
	}

	// create a zip archive
	func() {
		z, err := os.Create(pth)
		if err != nil {
			t.Errorf("expected no error creating zip archive, got %s", err)
		}
		defer z.Close()
		w := zip.NewWriter(z)
		f, err := w.Create("file.txt")
		if err != nil {
			t.Errorf("expected no error creating archived file, got %s", err)
		}
		defer w.Close()
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
	defer os.Remove(got)
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
	defer a.Close()
	r, err := a.Open("file.txt")
	if err != nil {
		t.Errorf("expected no error reading downloaded zip archive, got %s", err)
	}
	defer r.Close()
	var b bytes.Buffer
	if _, err := io.Copy(&b, r); err != nil {
		t.Errorf("expected no error reading archived file, got %s", err)
	}
	if !bytes.Equal(expected, b.Bytes()) {
		t.Error("archived contents differ from expected") // not printing becasuse it's a lot of data
	}
}

func TestDownload_Retry(t *testing.T) {
	timeout := 250 * time.Millisecond
	for _, tc := range []struct {
		desc string
		proc func(w http.ResponseWriter)
	}{
		{"failure", func(w http.ResponseWriter) { w.WriteHeader(http.StatusBadRequest) }},
		{"timeout", func(w http.ResponseWriter) { time.Sleep(10 * timeout) }},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			attempts := int32(0)
			s := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodHead {
						w.Header().Add("Content-Length", "2")
						return
					}
					if atomic.CompareAndSwapInt32(&attempts, 0, 1) {
						tc.proc(w)
					}
					fmt.Fprint(w, "42")
				},
			))
			defer s.Close()

			d := Downloader{
				OutputDir:            t.TempDir(),
				Timeout:              timeout,
				MaxRetries:           4,
				ConcurrencyPerServer: 1,
				ChunkSize:            1024,
				WaitRetry:            0 * time.Second,
			}
			ch := d.Download(s.URL)
			<-ch // discard the first status (just the file size)
			got := <-ch
			if got.Error != nil {
				t.Errorf("invalid error. want:nil got:%q", got.Error)
			}
			if attempts != 1 {
				t.Errorf("invalid number of attempts. want:1 got %d", attempts)
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
		})
	}
}

func TestDownloadWithContext_ErrorUserTimeout(t *testing.T) {
	userTimeout := 250 * time.Millisecond // please note that the user timeout is less than the timeout per chunk.
	timeout := 10 * userTimeout
	s := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.Header().Add("Content-Length", "2")
				return
			}
			time.Sleep(2 * userTimeout) // this time is greater than the user timeout, but shorter than the timeout per chunk.
		},
	))
	defer s.Close()
	d := Downloader{
		OutputDir:            t.TempDir(),
		Timeout:              timeout,
		MaxRetries:           4,
		ConcurrencyPerServer: 1,
		ChunkSize:            1024,
		WaitRetry:            0 * time.Second,
	}
	userCtx, cancFunc := context.WithTimeout(context.Background(), userTimeout)
	defer cancFunc()

	ch := d.DownloadWithContext(userCtx, s.URL)
	<-ch // discard the first got (just the file size)
	got := <-ch
	if got.Error == nil {
		t.Error("expected an error, but got nil")
	}
	if !strings.Contains(got.Error.Error(), "#4") {
		t.Error("expected #4 (configured number of retries), but did not get it")
	}
	if _, ok := <-ch; ok {
		t.Error("expected channel closed, but did not get it")
	}
}

func TestDownload_Chunks(t *testing.T) {
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

func TestGetDownloadSize_ContentLength(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "Test")
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
	attempts := int32(0)
	s := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if atomic.CompareAndSwapInt32(&attempts, 0, 1) {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			fmt.Fprint(w, "Test")
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
	s := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Range", "bytes 1-10/123")
			fmt.Fprint(w, "")
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
	d := DefaultDownloader()
	got, err := d.getDownloadSize(context.Background(), "test")

	if err == nil {
		t.Errorf("expected an error, got nil")
	}
	if got != 0 {
		t.Errorf("invalid size, expected 0, got: %d", got)
	}
}

func TestGetDownloadSize_NoContent(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "")
		},
	))
	defer s.Close()

	d := DefaultDownloader()
	if _, err := d.getDownloadSize(context.Background(), s.URL); err == nil {
		t.Error("expected error getting the file size, got nil")
	}
}

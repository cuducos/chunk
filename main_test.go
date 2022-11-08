package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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
					tc.proc(w)
				},
			))
			defer s.Close()
			d := Downloader{
				TimeoutPerChunk:               timeout,
				MaxRetriesPerChunk:            4,
				MaxParallelDownloadsPerServer: 1,
				ChunkSize:                     1024,
				WaitBetweenRetries:            0 * time.Second,
			}
			ch := d.Download(s.URL)
			status := <-ch
			if status.Error == nil {
				t.Error("expected an error, but got nil")
			}
			if !strings.Contains(status.Error.Error(), "#4") {
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
			fmt.Fprint(w, "42")
		},
	))
	defer s.Close()

	ch := DefaultDownloader().Download(s.URL)
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
					if atomic.CompareAndSwapInt32(&attempts, 0, 1) {
						tc.proc(w)
					}
					fmt.Fprint(w, "42")
				},
			))
			defer s.Close()

			d := Downloader{
				TimeoutPerChunk:               timeout,
				MaxRetriesPerChunk:            4,
				MaxParallelDownloadsPerServer: 1,
				ChunkSize:                     1024,
				WaitBetweenRetries:            0 * time.Second,
			}
			ch := d.Download(s.URL)
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

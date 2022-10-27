package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func testServer(t *testing.T) *httptest.Server {
	var attempt int32
	paths := make(map[string]func(http.ResponseWriter))
	paths["/ok"] = func(w http.ResponseWriter) {
		fmt.Fprintf(w, "42")
	}
	paths["/retry"] = func(w http.ResponseWriter) {
		if atomic.LoadInt32(&attempt) == 0 {
			atomic.StoreInt32(&attempt, 1)
			time.Sleep(1 * time.Second)
		}
		fmt.Fprintf(w, "42")
	}
	paths["/slow"] = func(w http.ResponseWriter) {
		time.Sleep(1 * time.Second)
	}

	return httptest.NewServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) {
				h, ok := paths[r.URL.Path]
				if !ok {
					t.Fatalf("unknown url path for the test server %s", r.URL.Path)
				}
				h(w)
			},
		),
	)
}

func TestGet(t *testing.T) {
	s := testServer(t)
	defer s.Close()
	for _, tc := range []struct {
		desc     string
		path     string
		expected []byte
	}{
		{"normal response", "/ok", []byte("42")},
		{"retried response", "/retry", []byte("42")},
		{"timeout", "/slow", nil},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			d := downloader{&http.Client{Timeout: 250 * time.Millisecond}, 3}
			got, err := d.download(s.URL + tc.path)
			if string(got) != string(tc.expected) {
				t.Errorf("expected %s, got %s", string(tc.expected), string(got))
			}
			if tc.expected == nil && err == nil {
				t.Error("expected an error, but got nil")
			}
			if tc.expected != nil && err != nil {
				t.Errorf("expected no error, but got %s", err)
			}
		})
	}
}

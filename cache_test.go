package main

import (
	"os"
	"testing"
)

const cacheTestFileName = "chunk.zip"

func TestCache_FromScratch(t *testing.T) {
	c, err := newCache(t.TempDir(), 5, false)
	if err != nil {
		t.Errorf("expected no error creating the cache, got %s", err)
	}
	c.addFile(cacheTestFileName, 4)
	c.chunkDone(cacheTestFileName, 2)
	for i := 0; i < 4; i++ {
		if i == 2 {
			continue
		}
		if !c.shouldDownload(cacheTestFileName, i) {
			t.Errorf("expected chunk %d to be pending, but it says it's downloaded", i)
		}
	}
	if c.shouldDownload(cacheTestFileName, 2) {
		t.Error("expected chunk 2 to be downloaded, but it says it's pending")
	}
	if err := c.close(); err != nil {
		t.Errorf("expected no errors saving the cache files, got %s", err)
	}
	for _, f := range c.caches {
		_, err := os.ReadFile(f.path)
		if err != nil {
			t.Errorf("expected no errors reading the cache file, got %s", err)
		}
	}
}

func TestCache_FromCacheWithValidChunkSize(t *testing.T) {
	tmp := t.TempDir()
	old, err := newCache(tmp, 5, false)
	if err != nil {
		t.Errorf("expected no error creating the cache, got %s", err)
	}
	old.addFile(cacheTestFileName, 4)
	old.chunkDone(cacheTestFileName, 2)
	old.close()

	r, err := newCache(tmp, 5, false)
	if err != nil {
		t.Errorf("expected no error creating the cache, got %s", err)
	}
	r.addFile(cacheTestFileName, 4)
	for i := 0; i < 4; i++ {
		got := r.shouldDownload(cacheTestFileName, i)
		if got && i == 2 {
			t.Errorf("expected chunk %d to be downloaded, but it says it's pending", i)
		}
		if !got && i != 2 {
			t.Errorf("expected chunk %d to be pending, but it says it's downloaded", i)
		}
	}
}

func TestCache_FromCacheWithInvalidChunkSize(t *testing.T) {
	tmp := t.TempDir()
	old, err := newCache(tmp, 5, false)
	if err != nil {
		t.Errorf("expected no error creating the cache, got %s", err)
	}
	old.addFile(cacheTestFileName, 4)
	old.chunkDone(cacheTestFileName, 2)
	old.close()

	_, err = newCache(tmp, 256, false)
	if err == nil {
		t.Error("expected error creating the cache with invalid chunk size, got nil")
	}
}

func TestCache_FromCacheWithRestart(t *testing.T) {
	tmp := t.TempDir()
	old, err := newCache(tmp, 5, false)
	if err != nil {
		t.Errorf("expected no error creating the cache, got %s", err)
	}
	old.addFile(cacheTestFileName, 4)
	old.chunkDone(cacheTestFileName, 2)
	old.close()

	r, err := newCache(tmp, 6, true)
	if err != nil {
		t.Errorf("expected no error creating the cache, got %s", err)
	}
	r.addFile(cacheTestFileName, 4)
	for i := 0; i < 4; i++ {
		if !r.shouldDownload(cacheTestFileName, i) {
			t.Errorf("expected chunk %d to be pending, but it says it's downloaded", i)
		}
	}
}

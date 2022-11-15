package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

const (
	cacheDirName       = ".cache"
	cacheChunkSizeName = ".chunk_size"
)

type cache struct {
	path      string
	chunkSize uint64
	restart   bool
	caches    map[string]*fileCache
	lock      sync.Mutex
}

func (c *cache) save() error {
	s := filepath.Join(c.path, cacheChunkSizeName)
	f, err := os.Create(s)
	if err != nil {
		return fmt.Errorf("error creating main cache file %s: %w", s, err)
	}
	defer f.Close()
	f.WriteString(fmt.Sprintf("%d", c.chunkSize))
	return nil
}
func (c *cache) load() error {
	pth := filepath.Join(c.path, cacheChunkSizeName)
	if c.restart {
		err := os.RemoveAll(pth)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("could not delete %s: %w", pth, err)
		}
		return c.save()
	}
	f, err := os.Open(pth)
	if os.IsNotExist(err) {
		return c.save()
	}
	if err != nil {
		return fmt.Errorf("error opening %s: %w", pth, err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return fmt.Errorf("could not read %s: %w", pth, err)
	}
	n, err := strconv.Atoi(string(b))
	if err != nil {
		return fmt.Errorf("could not parse %s as a chunk size", string(b))
	}
	if uint64(n) != c.chunkSize {
		return fmt.Errorf(
			"chunk size in %s is %d, but it is %d for the current download",
			c.path,
			n,
			c.chunkSize,
		)
	}
	c.chunkSize = uint64(n)
	return c.save()
}

func (c *cache) addFile(name string, numChunks int) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	f := fileCache{
		path:   filepath.Join(c.path, name),
		chunks: make([]uint32, numChunks),
	}
	if err := f.load(c.restart); err != nil {
		return fmt.Errorf("error loading cache for %s: %w", name, err)
	}
	c.caches[name] = &f
	return nil
}

func (c *cache) shouldDownload(name string, idx int) bool {
	f, ok := c.caches[name]
	if !ok {
		return false
	}
	return f.shouldDownload(idx)
}

func (c *cache) chunkDone(name string, idx int) {
	f, ok := c.caches[name]
	if !ok {
		return
	}
	f.chunkDone(idx)
}

func (c *cache) close() error {
	errs := make(chan error)
	for _, f := range c.caches {
		go func(f *fileCache) {
			errs <- f.save()
		}(f)
	}
	for range c.caches {
		if err := <-errs; err != nil {
			return fmt.Errorf("couldn't save cache file: %w", err)
		}
	}
	defer close(errs)
	for _, f := range c.caches {
		if !f.isDone() {
			return nil
		}
	}
	if err := os.RemoveAll(c.path); err != nil {
		return fmt.Errorf("error cleaning up cache files %s: %w", c.path, err)
	}
	return nil
}

func newCache(dir string, chunkSize uint64, restart bool) (*cache, error) {
	pth := filepath.Join(dir, cacheDirName)
	if err := os.MkdirAll(pth, 0750); err != nil {
		return nil, fmt.Errorf("error creating cache directory %s: %w", pth, err)
	}
	c := cache{
		path:      pth,
		chunkSize: chunkSize,
		restart:   restart,
		caches:    make(map[string]*fileCache),
	}
	if err := c.load(); err != nil {
		return nil, fmt.Errorf("error loading existing cache: %w", err)
	}
	return &c, nil
}

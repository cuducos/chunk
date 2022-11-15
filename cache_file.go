package main

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"
)

type fileCache struct {
	path   string
	chunks []uint32
}

func (c *fileCache) isValidIndex(idx int) bool { return idx >= 0 && idx < len(c.chunks) }

func (c *fileCache) shouldDownload(idx int) bool {
	if !c.isValidIndex(idx) {
		return false
	}
	return atomic.LoadUint32(&c.chunks[idx]) == 0
}

func (c *fileCache) save() error {
	f, err := os.Create(c.path)
	if err != nil {
		return fmt.Errorf("error opening cache file %s: %w", c.path, err)
	}
	defer f.Close()
	for i := range c.chunks {
		f.WriteString(fmt.Sprintf("%d", atomic.LoadUint32(&c.chunks[i])))
	}
	return nil
}

func (c *fileCache) load(restart bool) error {
	if restart {
		err := os.Remove(c.path)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("could not delete %s: %w", c.path, err)
		}
		return nil
	}
	f, err := os.Open(c.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("error opening %s: %w", c.path, err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		return fmt.Errorf("could not read %s: %w", c.path, err)
	}
	s := string(b)
	if len(s) != len(c.chunks) {
		return fmt.Errorf("cache %s has %d chunks, file has %d", c.path, len(s), len(c.chunks))
	}
	for idx, n := range s {
		if n == '1' {
			c.chunks[idx] = 1
		}
	}
	return nil
}

func (c *fileCache) chunkDone(idx int) {
	if !c.isValidIndex(idx) {
		return
	}
	atomic.StoreUint32(&c.chunks[idx], 1)
}

func (c *fileCache) isDone() bool {
	for idx := range c.chunks {
		if atomic.LoadUint32(&c.chunks[idx]) == 0 {
			return false
		}
	}
	return true
}

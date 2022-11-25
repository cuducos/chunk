package chunk

import (
	"crypto/md5"
	"encoding/gob"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// get the chunk directory under user's home directory
func getChunkDirectory() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("could not get current user: %w", err)
	}
	d := filepath.Join(u.HomeDir, ".chunk")
	if err := os.MkdirAll(d, 0755); err != nil {
		return "", fmt.Errorf("could not create chunk's directory %s: %w", d, err)
	}
	return d, nil
}

type progress struct {
	// path for persistence of this progress file
	path string
	lock sync.Mutex

	// download fields so the encoder/decoder has access to them
	URL       string
	Path      string
	ChunkSize int64
	Chunks    []uint32
}

// trues to loads a download progress from a file
func (p *progress) load(restart bool) error {
	p.lock.Lock()
	defer p.lock.Unlock()
	if restart {
		err := os.Remove(p.path)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("could not delete %s: %w", p.path, err)
		}
		return nil
	}
	f, err := os.Open(p.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("error opening %s: %w", p.path, err)
	}
	defer f.Close()
	d := gob.NewDecoder(f)
	var got progress
	if err := d.Decode(&got); err != nil {
		return fmt.Errorf("error decoding progress file %s: %w", p.path, err)
	}
	if got.URL != p.URL {
		return fmt.Errorf("download progress file %s has unexpected url %s, expected %s", p.path, got.URL, p.URL)
	}
	if got.Path != p.Path {
		return fmt.Errorf("download progress file %s has unexpected path %s, expected %s", p.path, got.Path, p.Path)
	}
	if got.ChunkSize != p.ChunkSize {
		return fmt.Errorf("download progress file %s has unexpected chunk size %d, expected %d", p.path, got.ChunkSize, p.ChunkSize)
	}
	if len(got.Chunks) != len(p.Chunks) {
		return fmt.Errorf("download progress file %s has unexpected number of chunks %d, expected %d", p.path, len(got.Chunks), len(p.Chunks))
	}
	p.Chunks = got.Chunks
	return nil
}

// checks if `idx` is a valid index for `p.Chunks` array
func (p *progress) isValidIndex(idx int) bool { return idx >= 0 && idx < len(p.Chunks) }

// checks if the chunk number `idx` should be downloaded (ie is not downloaded yet)
func (p *progress) shouldDownload(idx int) (bool, error) {
	if !p.isValidIndex(idx) {
		return false, fmt.Errorf("%s does not have chunk #%d", p.Path, idx+1)
	}
	return p.Chunks[idx] == 0, nil
}

// marks the chunk number `idx` as done (ie successfully downloaded)
func (p *progress) done(idx int) error {
	if !p.isValidIndex(idx) {
		return fmt.Errorf("%s does not have chunk #%d", p.Path, idx+1)
	}
	p.lock.Lock()
	defer p.lock.Unlock()
	atomic.StoreUint32(&p.Chunks[idx], 1)
	f, err := os.Create(p.path)
	if err != nil {
		return fmt.Errorf("error opening progress file %s: %w", p.path, err)
	}
	defer f.Close()
	e := gob.NewEncoder(f)
	if err := e.Encode(p); err != nil {
		return fmt.Errorf("error encoding progress file %s: %w", p.path, err)
	}
	return nil
}

func newProgress(path, url string, chunkSize int64, chunks int, restart bool) (*progress, error) {
	dir, err := getChunkDirectory()
	if err != nil {
		return nil, fmt.Errorf("could not get chunk's directory: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("could not get the download absolute path: %w", err)
	}
	hash := fmt.Sprintf("%x", md5.Sum([]byte(fmt.Sprintf("%s|%s", url, abs))))
	p := progress{
		path:      filepath.Join(dir, fmt.Sprintf("%s-%s", hash, filepath.Base(path))),
		URL:       url,
		Path:      abs,
		ChunkSize: chunkSize,
		Chunks:    make([]uint32, chunks),
	}
	if err := p.load(restart); err != nil {
		return nil, fmt.Errorf("error loading existing progress file: %w", err)
	}
	return &p, nil
}

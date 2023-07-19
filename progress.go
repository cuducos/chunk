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

// DefaultChunkDir is the directory where Chunk keeps track of each chunk
// downloaded of each file. It us created under the user's home directory by
// default. It can be replaced by the environment variable CHUNK_DIR.
const DefaultChunkDir = ".chunk"

// get the chunk directory under user's home directory
func getChunkProgressDir(dir string) (string, error) {
	if dir == "" {
		dir = os.Getenv("CHUNK_DIR")
	}
	if dir == "" {
		u, err := user.Current()
		if err != nil {
			return "", fmt.Errorf("could not get current user: %w", err)
		}
		dir = filepath.Join(u.HomeDir, DefaultChunkDir)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("could not create chunk's directory %s: %w", dir, err)
	}
	return dir, nil
}

type progress struct {
	// path for persistence of this progress file
	path string
	lock sync.Mutex

	// download fields so the encoder/decoder has access to them
	URL       string
	Path      string
	ChunkSize int64
	Chunks    []int64
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
func (p *progress) done(idx int, bytes int64) error {
	if !p.isValidIndex(idx) {
		return fmt.Errorf("%s does not have chunk #%d", p.Path, idx+1)
	}
	p.lock.Lock()
	defer p.lock.Unlock()
	atomic.StoreInt64(&p.Chunks[idx], bytes)
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

// check is all the chunks of the current download are done
func (p *progress) isDone() (bool, error) {
	for idx := range p.Chunks {
		s, err := p.shouldDownload(idx)
		if err != nil {
			return false, fmt.Errorf("error checking if chunk #%d for %s done: %w", idx+1, p.URL, err)
		}
		if s {
			return false, nil
		}
	}
	return true, nil
}

// removes this progress file it the download is done
func (p *progress) close() error {
	done, err := p.isDone()
	if err != nil {
		return fmt.Errorf("error checking if %s is done: %w", p.URL, err)
	}
	if done {
		if err := os.Remove(p.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("error cleaning up progress file %s: %w", p.path, err)
		}
	}
	return nil // Either not empty or error, suits both cases
}

// calculates the number of bytes downloaded
func (p *progress) downloadedBytes() int64 {
	var downloaded int64
	for _, c := range p.Chunks {
		downloaded += c
	}
	return downloaded
}

func newProgress(path, dir string, url string, chunkSize int64, chunks int, restart bool) (*progress, error) {
	dir, err := getChunkProgressDir(dir)
	if err != nil {
		return nil, fmt.Errorf("could not get chunk's directory: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("could not get the download absolute path: %w", err)
	}
	hash := fmt.Sprintf("%x", md5.Sum([]byte(fmt.Sprintf("%s|%s", url, abs))))

	// file name is a hash of the URL and local file path, plus the file name
	// in an human-readable way for debugging purposes
	name := fmt.Sprintf("%s-%s", hash, filepath.Base(path))
	p := progress{
		path:      filepath.Join(dir, name),
		URL:       url,
		Path:      abs,
		ChunkSize: chunkSize,
		Chunks:    make([]int64, chunks),
	}
	if err := p.load(restart); err != nil {
		return nil, fmt.Errorf("error loading existing progress file: %w", err)
	}
	return &p, nil
}

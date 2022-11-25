package chunk

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestProgress_FromScratch(t *testing.T) {
	tmp := t.TempDir()
	name := filepath.Join(tmp, "chunk.zip")
	p, err := newProgress(name, "https://test.etc/chunk.zip", 5, 3, false)
	if err != nil {
		t.Errorf("expected no error creating the progress, got %s", err)
	}
	if err := p.done(1); err != nil {
		t.Errorf("expected no error marking chunk as done, got %s", err)
	}
	for i := 0; i < 3; i++ {
		got, err := p.shouldDownload(i)
		if err != nil {
			t.Errorf("expected no error checking if chunk %d should be downloaded, got %s", i, err)
		}
		if i == 1 {
			if got {
				t.Errorf("expected chunk %d to be downloaded", i)
			}
		} else {
			if !got {
				t.Errorf("expected chunk %d not to be downloaded", i)
			}
		}
	}
	if err := p.close(); err != nil {
		t.Errorf("expected no errors saving the progress file, got %s", err)
	}
	if _, err := os.ReadFile(p.path); err != nil {
		t.Errorf("expected no errors reading the progress file, got %s", err)
	}
}

func TestProgress_ParallelComplete(t *testing.T) {
	tmp := t.TempDir()
	name := filepath.Join(tmp, "chunk.zip")
	p, err := newProgress(name, "https://test.etc/chunk.zip", 5, 2048, false)
	if err != nil {
		t.Errorf("expected no error creating the progress, got %s", err)
	}
	var wg sync.WaitGroup
	errs := make(chan error)
	for i := 0; i < 2048; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- p.done(i)
		}(i)
	}
	for i := 0; i < 2048; i++ {
		err := <-errs
		if err != nil {
			t.Errorf("expected no error marking chunk as done, got %s", err)
		}
	}
	close(errs)
	if err := p.close(); err != nil {
		t.Errorf("expected no errors removing the progress file, got %s", err)
	}
	if _, err := os.ReadFile(p.path); !os.IsNotExist(err) {
		t.Errorf("expected progress file not to exist, rediong it returned error %s", err)
	}
}

func TestProgress_FromFile(t *testing.T) {
	tmp := t.TempDir()
	name := filepath.Join(tmp, "chunk.zip")
	old, err := newProgress(name, "https://test.etc/chunk.zip", 5, 3, false)
	if err != nil {
		t.Errorf("expected no error creating the old progress, got %s", err)
	}
	old.done(1)
	old.close()

	p, err := newProgress(name, "https://test.etc/chunk.zip", 5, 3, false)
	if err != nil {
		t.Errorf("expected no error creating the progress, got %s", err)
	}
	for i := 0; i < 3; i++ {
		got, err := p.shouldDownload(i)
		if err != nil {
			t.Errorf("expected no error checking if chunk %d should be downloaded, got %s", i, err)
		}
		if i == 1 {
			if got {
				t.Errorf("expected chunk %d to be downloaded", i)
			}
		} else {
			if !got {
				t.Errorf("expected chunk %d not to be downloaded", i)
			}
		}
	}
	if err := p.close(); err != nil {
		t.Errorf("expected no errors saving the progress file, got %s", err)
	}
	if _, err := os.ReadFile(p.path); err != nil {
		t.Errorf("expected no errors reading the progress file, got %s", err)
	}
}

func TestProgress_FromFileWithInvalidChunkSize(t *testing.T) {
	tmp := t.TempDir()
	name := filepath.Join(tmp, "chunk.zip")
	old, err := newProgress(name, "https://test.etc/chunk.zip", 5, 3, false)
	if err != nil {
		t.Errorf("expected no error creating the old progress, got %s", err)
	}
	old.done(1)
	old.close()

	if _, err := newProgress(name, "https://test.etc/chunk.zip", 10, 3, false); err == nil {
		t.Error("expected error creating the progress with different chunk size")
	}
}

func TestProgress_FromFileWithRestart(t *testing.T) {
	tmp := t.TempDir()
	name := filepath.Join(tmp, "chunk.zip")
	old, err := newProgress(name, "https://test.etc/chunk.zip", 5, 3, false)
	if err != nil {
		t.Errorf("expected no error creating the old progress, got %s", err)
	}
	old.done(1)
	old.close()

	p, err := newProgress(name, "https://test.etc/chunk.zip", 10, 3, true)
	if err != nil {
		t.Errorf("expected no error creating the progress, got %s", err)
	}
	for i := 0; i < 3; i++ {
		got, err := p.shouldDownload(i)
		if err != nil {
			t.Errorf("expected no error checking if chunk %d should be downloaded, got %s", i, err)
		}
		if !got {
			t.Errorf("expected chunk %d not to be downloaded", i)
		}
	}
}

package main

import (
	"context"
	"fmt"
	"time"
)

func humanSize(s float64) string {
	size, unit := func(s float64) (float64, string) {
		m := []string{"B", "kB", "MB", "GB", "TB"}
		i := 0
		b := 1000.0
		for s >= b && i < len(m)-1 {
			s = s / b
			i++
		}
		return s, m[i]
	}(s)
	return fmt.Sprintf("%.1f%s", size, unit)
}

type file struct{ total, done int64 }

type downloadProgress struct {
	files     map[string]file
	startedAt time.Time
	close     context.CancelFunc
}

func (p *downloadProgress) total() (t int64) {
	for _, f := range p.files {
		t += f.total
	}
	return
}

func (p *downloadProgress) done() (d int64) {
	for _, f := range p.files {
		d += f.done
	}
	return
}

func (p *downloadProgress) String() string {
	perc := float64(p.done()) / float64(p.total())
	speed := float64(p.done()) / time.Since(p.startedAt).Seconds()
	return fmt.Sprintf(
		"Downloading %s of %s\t%.2f%%\t%s/s",
		humanSize(float64(p.done())),
		humanSize(float64(p.total())),
		perc*100,
		humanSize(speed),
	)
}

func (p *downloadProgress) update(d DownloadStatus) {
	p.files[d.DownloadedFilePath] = file{d.FileSizeBytes, d.DownloadedFileBytes}
	if p.done() == p.total() {
		p.close()
	}
}

func NewProgress() *downloadProgress {
	ctx, cancel := context.WithCancel(context.Background())
	bar := downloadProgress{make(map[string]file), time.Now(), cancel}
	tick := time.Tick(1 * time.Second)
	go func() {
		for {
			select {
			case <-tick:
				fmt.Printf("\r%s", bar.String())
			case <-ctx.Done():
				return
			}
		}
	}()
	return &bar
}

package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/avast/retry-go"
)

const (
	defaultRetries = 3
	defaultTimeout = 45 * time.Second
)

type downloader struct {
	client  *http.Client
	retries uint
}

func (d *downloader) downloadWithContext(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating the request for %s: %w", u, err)
	}
	req = req.WithContext(ctx)
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending a get http request to %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("got http response %s from %s: %w", resp.Status, u, err)
	}
	var b bytes.Buffer
	_, err = b.ReadFrom(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading body from %s: %w", u, err)
	}
	return b.Bytes(), nil
}

func (d *downloader) downloadWithTimeout(u string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d.client.Timeout)
	defer cancel()
	ch := make(chan []byte)
	errs := make(chan error)
	go func() {
		b, err := d.downloadWithContext(ctx, u)
		if err != nil {
			errs <- err
			return
		}
		ch <- b
	}()
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("request to %s ended due to timeout: %w", u, ctx.Err())
	case err := <-errs:
		return nil, fmt.Errorf("request to %s failed: %w", u, err)
	case b := <-ch:
		return b, nil
	}
}

func (d *downloader) download(u string) ([]byte, error) {
	ch := make(chan []byte, 1)
	defer close(ch)
	err := retry.Do(
		func() error {
			b, err := d.downloadWithTimeout(u)
			if err != nil {
				return err
			}
			ch <- b
			return nil
		},
		retry.Attempts(d.retries),
		retry.MaxDelay(d.client.Timeout),
	)
	if err != nil {
		return nil, fmt.Errorf("error downloading %s: %w", u, err)
	}
	b := <-ch
	return b, nil
}

func main() {
	d := downloader{&http.Client{Timeout: defaultTimeout}, uint(defaultRetries)}
	b, err := d.download(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(string(b))
}

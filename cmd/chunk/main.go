package main

import (
	"fmt"
	"log"
	"os"

	"github.com/cuducos/chunk"
)

func main() {
	chunk := chunk.DefaultDownloader()
	prog := newProgress()
	for status := range chunk.Download(os.Args[1:len(os.Args)]...) {
		if status.Error != nil {
			log.Fatal(status.Error)
		}
		prog.update(status)
	}
	fmt.Printf("\r%s\nDownloaded to: %s", prog.String(), os.TempDir())
}

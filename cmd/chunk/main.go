package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cuducos/chunk"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "chunk",
	Short: "Download tool for slow and unstable servers",
	Long:  "Download tool for slow and unstable servers using HTTP range requests, retries per HTTP request (not by file), prevents re-downloading the same content range and supports wait time to give servers time to recover.",
	Run: func(cmd *cobra.Command, args []string) {
		chunk := chunk.DefaultDownloader()
		chunk.Timeout = timeoutChunk
		chunk.ConcurrencyPerServer = concurrencyPerServer
		chunk.MaxRetries = maxRetriesChunk
		chunk.WaitRetry = waitBetweenRetries
		chunk.ChunkSize = chunkSize
		prog := newProgress()
		for status := range chunk.Download(os.Args[1:len(os.Args)]...) {
			if status.Error != nil {
				log.Fatal(status.Error)
			}
			prog.update(status)
		}
		fmt.Printf("\r%s\nDownloaded to: %s", prog.String(), os.TempDir())
	},
}

// Flags
var (
	timeoutChunk         time.Duration
	concurrencyPerServer int
	maxRetriesChunk      uint
	chunkSize            int64
	waitBetweenRetries   time.Duration
)

func init() {
	rootCmd.Flags().DurationVarP(&timeoutChunk, "timeout", "t", chunk.DefaultTimeout, "timeout for the download of each chunk from each URL.")
	rootCmd.Flags().UintVarP(&maxRetriesChunk, "max-retries", "r", chunk.DefaultMaxRetries, "maximum number of retries for each chunk.")
	rootCmd.Flags().DurationVarP(&waitBetweenRetries, "wait-retry", "w", chunk.DefaultWaitRetry, "pause before retrying an HTTP request that has failed.")
	rootCmd.Flags().Int64VarP(&chunkSize, "chunk-size", "s", chunk.DefaultChunkSize, "maximum size of each HTTP request done using the content range header.")
	rootCmd.Flags().IntVarP(&concurrencyPerServer, "concurrency-per-server", "c", chunk.DefaultConcurrencyPerServer, "controls the max number of concurrent connections opened to the same server.")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

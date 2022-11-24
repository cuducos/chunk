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
	Long:  `The idea of the project emerged as it was difficult for Minha Receita to handle the download of 37 files that adds up to just approx. 5Gb. Most of the download solutions out there (e.g. got) seem to be prepared for downloading large files, not for downloading from slow and unstable servers — which is the case at hand.`,
	Run: func(cmd *cobra.Command, args []string) {
		chunk := chunk.DefaultDownloader()
		chunk.TimeoutPerChunk = timeoutChunk
		chunk.MaxParallelDownloadsPerServer = concurrencyPerServer
		chunk.MaxRetriesPerChunk = maxRetriesChunk
		chunk.WaitBetweenRetries = waitBetweenRetries
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
	rootCmd.Flags().DurationVarP(&timeoutChunk, "timeout", "t", chunk.DefaultTimeoutPerChunk, "timeout for the download of each chunk from each URL.")
	rootCmd.Flags().UintVarP(&maxRetriesChunk, "max-retries", "r", chunk.DefaultMaxRetriesPerChunk, "maximum number of retries for each chunk.")
	rootCmd.Flags().DurationVarP(&waitBetweenRetries, "wait-between-retries", "w", chunk.DefaultWaitBetweenRetries, "pause before retrying an HTTP request that has failed.")
	rootCmd.Flags().Int64VarP(&chunkSize, "chunk-size", "s", chunk.DefaultChunkSize, "maximum size of each HTTP request done using the content range header.")
	rootCmd.Flags().IntVarP(&concurrencyPerServer, "concurrency-per-server", "c", chunk.DefaultMaxParallelDownloadsPerServer, "controls the max number of concurrent connections opened to the same server.")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

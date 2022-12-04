package main

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cuducos/chunk"
	"github.com/spf13/cobra"
)

func checkOutputDir() error {
	var err error
	if outputDir == "" {
		outputDir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("could not load current directory: %w", err)
		}
	}
	i, err := os.Stat(outputDir)
	if os.IsNotExist(err) {
		return fmt.Errorf("directory %s does not exist", outputDir)
	}
	if err != nil {
		return fmt.Errorf("could not get info for %s: %w", outputDir, err)
	}
	if !i.Mode().IsDir() {
		return fmt.Errorf("%s is not a directory", outputDir)
	}
	return nil
}

var rootCmd = &cobra.Command{
	Use:   "chunk",
	Short: "Download tool for slow and unstable servers",
	Long:  "Download tool for slow and unstable servers using HTTP range requests, retries per HTTP request (not by file), prevents re-downloading the same content range and supports wait time to give servers time to recover.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := checkOutputDir(); err != nil {
			return fmt.Errorf("error checking %s directory: %w", outputDir, err)
		}
		chunk := chunk.DefaultDownloader()
		chunk.OutputDir = outputDir
		chunk.Timeout = timeoutChunk
		chunk.ConcurrencyPerServer = concurrencyPerServer
		chunk.MaxRetries = maxRetriesChunk
		chunk.WaitRetry = waitBetweenRetries
		chunk.ChunkSize = chunkSize
		chunk.RestartDownloads = restartDownloads
		prog := newProgress()
		for status := range chunk.Download(args...) {
			if status.Error != nil {
				log.Fatal(status.Error)
			}
			prog.update(status)
		}
		return nil
	},
}

// Flags
var (
	outputDir            string
	timeoutChunk         time.Duration
	concurrencyPerServer int
	maxRetriesChunk      uint
	chunkSize            int64
	waitBetweenRetries   time.Duration
	restartDownloads     bool
	progressDir          string
)

func init() {
	rootCmd.Flags().StringVarP(&outputDir, "output-directory", "d", "", "directory where to save the downloaded files.")
	rootCmd.Flags().DurationVarP(&timeoutChunk, "timeout", "t", chunk.DefaultTimeout, "timeout for the download of each chunk from each URL.")
	rootCmd.Flags().UintVarP(&maxRetriesChunk, "max-retries", "r", chunk.DefaultMaxRetries, "maximum number of retries for each chunk.")
	rootCmd.Flags().DurationVarP(&waitBetweenRetries, "wait-retry", "w", chunk.DefaultWaitRetry, "pause before retrying an HTTP request that has failed.")
	rootCmd.Flags().Int64VarP(&chunkSize, "chunk-size", "s", chunk.DefaultChunkSize, "maximum size of each HTTP request done using the content range header.")
	rootCmd.Flags().IntVarP(&concurrencyPerServer, "concurrency-per-server", "c", chunk.DefaultConcurrencyPerServer, "controls the max number of concurrent connections opened to the same server.")
	rootCmd.Flags().BoolVarP(&restartDownloads, "force-restart", "f", chunk.DefaultRestartDownload, "restart previous downloads, ignoring where they were stopped")
	rootCmd.Flags().StringVarP(
		&progressDir,
		"progress-directory",
		"p",
		"",
		fmt.Sprintf("directory where to track progress; %s under user's home directory if blank, or CHUNK_DIR environment variable", chunk.DefaultChunkDir),
	)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

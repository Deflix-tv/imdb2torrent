package main

import (
	"context"
	"fmt"

	"github.com/deflix-tv/imdb2torrent"
	"go.uber.org/zap"
)

func main() {
	// Prepare client parameters
	opts := imdb2torrent.DefaultYTSclientOpts
	cache := imdb2torrent.NewInMemoryCache()
	logger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}

	// Create new client
	yts := imdb2torrent.NewYTSclient(opts, cache, logger, false)

	// Fetch torrents for a movie
	imdbID := "tt0063350" // "Night of the Living Dead" from 1968, which is in the public domain
	torrents, err := yts.FindMovie(context.Background(), imdbID)
	if err != nil {
		panic(err)
	}
	if len(torrents) == 0 {
		fmt.Println("No torrents found")
	}

	// Iterate through results and print their magnet URLs
	for _, torrent := range torrents {
		fmt.Printf("Found torrent: %v\n", torrent.MagnetURL)
	}
}

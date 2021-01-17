# imdb2torrent

Go library for finding torrents for movies and TV shows by IMDb ID on YTS, The Pirate Bay, 1337x, RARBG and ibit

## Usage

You can either use a site-specific client:

```go
package main

import (
    "context"
    "fmt"

    "github.com/deflix-tv/imdb2torrent"
    "go.uber.org/zap"
)

func main() {
    // Create new client
    yts := imdb2torrent.NewYTSclient(imdb2torrent.DefaultYTSclientOpts, imdb2torrent.NewInMemoryCache(), zap.NewNop(), false)

    // Fetch torrents for a movie
    // Here we use the IMDb ID of "Night of the Living Dead" from 1968, which is in the public domain
    torrents, err := yts.FindMovie(context.Background(), "tt0063350")
    if err != nil {
        panic(err)
    }

    // Iterate through results and print their magnet URLs
    for _, torrent := range torrents {
        fmt.Printf("Found torrent: %v\n", torrent.MagnetURL)
    }
}
```

Or use the `imdb2torrent.Client`, which uses multiple site-specific clients _concurrently_.

For more detailed examples see [examples](examples).

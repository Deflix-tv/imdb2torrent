package imdb2torrent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRARBGMovie(t *testing.T) {
	cache := NewInMemoryCache()
	logger := zap.NewNop()

	client := NewRARBGclient(DefaultRARBGclientOpts, cache, logger, false)

	imdbID := "tt0063350" // Night of the Living Dead, 1968, public domain
	torrents, err := client.FindMovie(context.Background(), imdbID)
	require.NoError(t, err)
	require.NotEmpty(t, torrents)

	firstElem := torrents[0]
	fmt.Printf("RARBG result first elem: %+v\n", firstElem)
	require.NotEmpty(t, firstElem.Name)
	require.Len(t, firstElem.InfoHash, 40)
	require.True(t, strings.HasPrefix(firstElem.MagnetURL, "magnet:?xt=urn:btih:"+firstElem.InfoHash))
	require.Regexp(t, qualityRegex, firstElem.Quality)
	// RARBG response doesn't contain a title, and it doesn't use the metaGetter
	// require.Equal(t, firstElem.Title, "Night of the Living Dead")
	require.Greater(t, firstElem.Size, 0)
}

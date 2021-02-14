package imdb2torrent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestTPBMovie(t *testing.T) {
	imdbID := "tt0063350" // Night of the Living Dead, 1968, public domain

	cache := NewInMemoryCache()
	metaGetter := &metaGetterMock{
		t:         t,
		expIMDBID: imdbID,
		meta: Meta{
			Title: "Night of the Living Dead",
			Year:  1968,
		},
	}
	logger := zap.NewNop()

	client, err := NewTPBclient(DefaultTPBclientOpts, cache, metaGetter, logger, false)
	require.NoError(t, err)

	torrents, err := client.FindMovie(context.Background(), imdbID)
	require.NoError(t, err)
	require.NotEmpty(t, torrents)

	firstElem := torrents[0]
	fmt.Printf("TPB result first elem: %+v\n", firstElem)
	require.NotEmpty(t, firstElem.Name)
	require.Len(t, firstElem.InfoHash, 40)
	require.True(t, strings.HasPrefix(firstElem.MagnetURL, "magnet:?xt=urn:btih:"+firstElem.InfoHash))
	require.Regexp(t, qualityRegex, firstElem.Quality)
	require.Equal(t, firstElem.Title, "Night of the Living Dead")
	require.Greater(t, firstElem.Size, 0)
}

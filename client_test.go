package imdb2torrent

import (
	"context"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

var qualityRegex = regexp.MustCompile("^(720p|1080p|2160p).*$")

type metaGetterMock struct {
	t *testing.T

	expIMDBID             string
	expSeason, expEpisode int
	meta                  Meta
}

func (c *metaGetterMock) GetMovieSimple(ctx context.Context, imdbID string) (Meta, error) {
	require.Equal(c.t, c.expIMDBID, imdbID)
	return c.meta, nil
}

func (c *metaGetterMock) GetTVShowSimple(ctx context.Context, imdbID string, season, episode int) (Meta, error) {
	require.Equal(c.t, c.expIMDBID, imdbID)
	require.Equal(c.t, c.expSeason, season)
	require.Equal(c.t, c.expEpisode, episode)
	return c.meta, nil
}

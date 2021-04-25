package imdb2torrent

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

var (
	magnet2InfoHashRegex = regexp.MustCompile(`btih:.+?&`)     // The "?" makes the ".+" non-greedy
	regexMagnet          = regexp.MustCompile(`'magnet:?.+?'`) // The "?" makes the ".+" non-greedy
)

type Meta struct {
	Title string
	Year  int
}

type MetaGetter interface {
	GetMovieSimple(ctx context.Context, imdbID string) (Meta, error)
	GetTVShowSimple(ctx context.Context, imdbID string, season, episode int) (Meta, error)
}

type MagnetSearcher interface {
	FindMovie(ctx context.Context, imdbID string) ([]Result, error)
	FindTVShow(ctx context.Context, imdbID string, season, episode int) ([]Result, error)
	IsSlow() bool
}

type findFunc func(context.Context, MagnetSearcher) ([]Result, error)

type Client struct {
	timeout     time.Duration
	slowTimeout time.Duration
	siteClients map[string]MagnetSearcher
	logger      *zap.Logger
}

// The timeouts here are not the ones used in the site-specific clients (for their HTTP requests).
// Instead they're used as limit for waiting for individual clients' results, as all of the searches are started concurrently.
// So for example while the 1337x client makes multiple requests, each with their respective timeout, the timeout here will
// be the max time that this "multi"-client waits for a result.
//
// timeout is the regular timeout for clients that are expected to respond quickly.
// slowTimeout is the timeout for clients that make themselves known as being slow.
// For example the RARBG rate limit is 2s, so when we don't do any request for 15m and thus need to renew the token,
// the client has to wait 2s for the actual torrent request, which might be longer than you want.
// Seeting it to 2s leads to only getting RARBG results when 1. the token is fresh and 2. no concurrent requests are done
// (the latter because requests to RARBG get queued so we don't exceed the rate limit with concurrent requests).
// You might wonder why not use a single timeout value, but the separation is useful for example if you *expect* the fast clients
// to respond in under a second in most cases, but want to allow them 5s for some irregular circumstances,
// while you know ibit can take 10s and more, but for the average case you only want to wait 2s max (and not 5s).
func NewClient(siteClients map[string]MagnetSearcher, timeout, slowTimeout time.Duration, logger *zap.Logger) *Client {
	return &Client{
		timeout:     timeout,
		slowTimeout: slowTimeout,
		siteClients: siteClients,
		logger:      logger,
	}
}

// FindMovie tries to find magnet URLs for the movie identified by the given IMDb ID.
// It only returns 720p, 1080p, 1080p 10bit, 2160p and 2160p 10bit videos.
// It caches results once they're found.
// It can return an empty slice and no error if no actual error occurred (for example if torrents where found but no >=720p videos).
func (c *Client) FindMovie(ctx context.Context, imdbID string) ([]Result, error) {
	find := func(ctx context.Context, siteClient MagnetSearcher) ([]Result, error) {
		return siteClient.FindMovie(ctx, imdbID)
	}
	return c.find(ctx, imdbID, find)
}

// FindTVShow tries to find magnet URLs for the TV show identified by the given IMDb ID + season + episode.
// It only returns 720p, 1080p, 1080p 10bit, 2160p and 2160p 10bit videos.
// It caches results once they're found.
// It can return an empty slice and no error if no actual error occurred (for example if torrents where found but no >=720p videos).
func (c *Client) FindTVShow(ctx context.Context, imdbID string, season, episode int) ([]Result, error) {
	find := func(ctx context.Context, siteClient MagnetSearcher) ([]Result, error) {
		return siteClient.FindTVShow(ctx, imdbID, season, episode)
	}
	id := imdbID + ":" + strconv.Itoa(season) + ":" + strconv.Itoa(episode)
	return c.find(ctx, id, find)
}

func (c *Client) find(ctx context.Context, id string, find findFunc) ([]Result, error) {
	zapFieldID := zap.String("id", id)

	clientCount := len(c.siteClients)
	resChan := make(chan []Result, clientCount)
	errChan := make(chan error, clientCount)

	// Start all clients' searches in parallel.

	for siteName, siteClient := range c.siteClients {
		// We need to create a new timer for each site client because a timer's channel is drained once used, so for example if these timers were created outside the loop and there are two slow (IsSlow()==true) clients, the timeout would only work for one of them!
		var timer *time.Timer
		if siteClient.IsSlow() {
			timer = time.NewTimer(c.slowTimeout)
		} else {
			timer = time.NewTimer(c.timeout)
		}

		// Note: Let's not close the channels in the senders, as it would make the receiver's code more complex. The GC takes care of that.
		go func(siteName string, siteClient MagnetSearcher, timer *time.Timer) {
			defer timer.Stop()

			zapFieldTorrentSite := zap.String("torrentSite", siteName)
			c.logger.Debug("Finding torrents...", zapFieldID, zapFieldTorrentSite)
			siteResChan := make(chan []Result)
			siteErrChan := make(chan error)
			go func() {
				siteStart := time.Now()
				results, err := find(ctx, siteClient)
				if err != nil {
					c.logger.Warn("Couldn't find torrents", zap.Error(err), zapFieldID, zapFieldTorrentSite)
					siteErrChan <- err
				} else {
					duration := time.Since(siteStart).Milliseconds()
					durationString := strconv.FormatInt(duration, 10)
					c.logger.Debug("Found torrents", zap.Int("torrentCount", len(results)), zap.String("duration", durationString+"ms"), zapFieldID, zapFieldTorrentSite)
					siteResChan <- results
				}
			}()
			select {
			case res := <-siteResChan:
				resChan <- res
			case err := <-siteErrChan:
				errChan <- err
			case <-timer.C:
				if siteClient.IsSlow() {
					c.logger.Info("Finding torrents timed out. It will continue to run in the background.", zapFieldID, zapFieldTorrentSite)
				} else {
					c.logger.Warn("Finding torrents timed out. It will continue to run in the background.", zapFieldID, zapFieldTorrentSite)
				}
				resChan <- nil
			}
		}(siteName, siteClient, timer)
	}

	// Collect results from all clients.

	var combinedResults []Result
	var errs []error
	dupRemovalRequired := false
	// For each client we get either a result or an error.
	// The timeout is handled in the site specific goroutine, because if we would use it here, and there were 4 clients and a timeout of 5 seconds, it could lead to 4*5=20 seconds of waiting time.
	for i := 0; i < clientCount; i++ {
		select {
		case results := <-resChan:
			if !dupRemovalRequired && len(combinedResults) > 0 && len(results) > 0 {
				dupRemovalRequired = true
			}
			combinedResults = append(combinedResults, results...)
		case err := <-errChan:
			errs = append(errs, err)
		}
	}

	returnErrors := len(errs) == clientCount

	// Return error (only) if all torrent sites returned actual errors (and not just empty results)
	if returnErrors {
		errsMsg := "Couldn't find torrents on any site: "
		for i := 1; i <= clientCount; i++ {
			errsMsg += fmt.Sprintf("%v.: %v; ", i, errs[i-1])
		}
		errsMsg = strings.TrimSuffix(errsMsg, "; ")
		return nil, fmt.Errorf(errsMsg)
	}

	// Remove duplicates.
	// Only necessary if we got non-empty results from more than one torrent site.
	var noDupResults []Result
	if dupRemovalRequired {
		infoHashes := map[string]struct{}{}
		for _, result := range combinedResults {
			if _, ok := infoHashes[result.InfoHash]; !ok {
				noDupResults = append(noDupResults, result)
				infoHashes[result.InfoHash] = struct{}{}
			}
		}
	} else {
		noDupResults = combinedResults
	}

	if len(noDupResults) == 0 {
		c.logger.Warn("Couldn't find ANY torrents", zapFieldID)
	}

	return noDupResults, nil
}

func (c *Client) GetMagnetSearchers() map[string]MagnetSearcher {
	return c.siteClients
}

type Result struct {
	// Name of the torrent, as given on the torrent site.
	// E.g. "Night of the Living Dead (1968) [BluRay] [1080p] [YTS] [YIFY]"
	// Or "Night.Of.The.Living.Dead.1968.720p.BluRay.x264-CtrlHD [PublicHD]"
	Name string
	// Movie title, e.g. "Night of the Living Dead"
	Title string
	// Video resolution and source, e.g. "720p" or "720p (web)"
	Quality string
	// Torrent info_hash. Lowercase.
	InfoHash string
	// MagnetURL, usually containing the info_hash (can be uppercase), torrent name and a list of torrent trackers
	MagnetURL string
	// True if the client didn't do the search via IMDb ID but title, which can lead to inaccurate results
	Fuzzy bool
	// Size in bytes. 0 if it couldn't be determined.
	Size int
	// Number of seeders according to the torrent site
	Seeders int
}

func replaceURL(origURL, newBaseURL string) (string, error) {
	// Replace by configured URL, which could be a proxy that we want to go through
	url, err := url.Parse(origURL)
	if err != nil {
		return "", fmt.Errorf("Couldn't parse URL. URL: %v; error: %v", origURL, err)
	}
	origBaseURL := url.Scheme + "://" + url.Host
	return strings.Replace(origURL, origBaseURL, newBaseURL, 1), nil
}

func createMagnetURL(ctx context.Context, infoHash, title string, trackers []string) string {
	magnetURL := "magnet:?xt=urn:btih:" + infoHash + "&dn=" + url.QueryEscape(title)
	for _, tracker := range trackers {
		magnetURL += "&tr" + tracker
	}
	return magnetURL
}

func createTVShowSearch(ctx context.Context, metaGetter MetaGetter, imdbID string, season, episode int) (string, error) {
	id := imdbID + ":" + strconv.Itoa(season) + ":" + strconv.Itoa(episode)
	meta, err := metaGetter.GetTVShowSimple(ctx, imdbID, season, episode)
	if err != nil {
		return "", fmt.Errorf("Couldn't get TV show title via Cinemeta for ID %v: %v", id, err)
	}
	seasonString := strconv.Itoa(season)
	episodeString := strconv.Itoa(episode)
	if season < 10 {
		seasonString = "0" + seasonString
	}
	if episode < 10 {
		episodeString = "0" + episodeString
	}
	return fmt.Sprintf("%v S%vE%v", meta.Title, seasonString, episodeString), nil
}

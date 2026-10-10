package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Bounds on fetching a referenced document. They are fixed rather than
// flags: they exist to stop a hostile or broken server, not to be tuned.
const (
	// fetchTimeout covers one whole request, connect to last byte. A spec
	// file arrives in well under a second; a server that takes this long is
	// not going to finish.
	fetchTimeout = 15 * time.Second
	// maxFetchSize caps one fetched document. The largest public specs are a
	// few megabytes.
	maxFetchSize = 16 << 20
	// maxRedirects caps how far a fetch may be sent on.
	maxRedirects = 5
)

// opener reads the documents a spec refers to. Files are always read.
// https URLs are fetched only when remote is set, which is the user saying
// so with --allow-remote-refs.
type opener struct {
	remote bool
	client *http.Client
	agent  string
}

func newOpener(remote bool) *opener {
	return &opener{
		remote: remote,
		agent:  "bollworm/" + version,
		client: &http.Client{
			Timeout: fetchTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= maxRedirects {
					return fmt.Errorf("more than %d redirects", maxRedirects)
				}
				if req.URL.Scheme != "https" {
					return fmt.Errorf("redirected to %s, which is not https", req.URL)
				}
				return nil
			},
		},
	}
}

var errRemoteOff = errors.New("fetching it needs the network; pass --allow-remote-refs to allow that, or bundle the document into one file")

func (o *opener) open(loc string) ([]byte, error) {
	if !strings.HasPrefix(loc, "https://") {
		return os.ReadFile(loc)
	}
	if !o.remote {
		return nil, errRemoteOff
	}
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loc, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", o.agent)
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: %s", loc, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchSize+1))
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", loc, err)
	}
	if len(body) > maxFetchSize {
		return nil, fmt.Errorf("fetching %s: larger than %d MiB", loc, maxFetchSize>>20)
	}
	return body, nil
}

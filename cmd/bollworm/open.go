package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"strconv"
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
	// fetchAttempts is how many times a fetch is tried when the failure
	// looks passing: a dropped connection, 429, or a 5xx other than 501.
	// CI runners share networks and rate limits, and one blip should not
	// fail a build. A fetch that runs out of time is not tried again: a
	// server that slow would make every attempt cost the whole timeout.
	fetchAttempts = 3
	// firstRetryWait is the wait before the second attempt; it doubles
	// before each attempt after that, and each wait is jittered by up to
	// half either way so parallel CI jobs do not retry in step.
	firstRetryWait = time.Second
	// maxRetryWait caps how long a server's Retry-After is honoured.
	maxRetryWait = 10 * time.Second
)

// opener reads the documents a spec refers to. Files are always read.
// https URLs are fetched only when remote is set, which is the user saying
// so with --allow-remote-refs.
type opener struct {
	remote bool
	client *http.Client
	agent  string
	// sleep waits between attempts, and jitter returns a factor in
	// [0.5, 1.5) for each wait; tests replace both.
	sleep  func(time.Duration)
	jitter func() float64
}

func newOpener(remote bool) *opener {
	return &opener{
		remote: remote,
		agent:  "bollworm/" + version,
		sleep:  time.Sleep,
		jitter: func() float64 { return 0.5 + rand.Float64() },
		client: &http.Client{
			Timeout: fetchTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= maxRedirects {
					return &redirectError{fmt.Sprintf("more than %d redirects", maxRedirects)}
				}
				if req.URL.Scheme != "https" {
					return &redirectError{fmt.Sprintf("redirected to %s, which is not https", req.URL)}
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
	var err error
	for attempt := 1; ; attempt++ {
		var body []byte
		var wait time.Duration
		body, wait, err = o.fetch(loc)
		if err == nil {
			return body, nil
		}
		if wait < 0 || attempt == fetchAttempts {
			break
		}
		if wait == 0 {
			wait = time.Duration(float64(firstRetryWait<<(attempt-1)) * o.jitter())
		}
		o.sleep(wait)
	}
	if fetchAttempts > 1 {
		var t transient
		if errors.As(err, &t) {
			return nil, fmt.Errorf("%w (tried %d times)", err, fetchAttempts)
		}
	}
	return nil, err
}

// transient marks a failure worth trying again.
type transient struct{ error }

func (t transient) Unwrap() error { return t.error }

// fetch makes one attempt. A failure worth retrying comes with the wait the
// server asked for, or 0 to back off; any other failure comes with a
// negative wait.
func (o *opener) fetch(loc string) ([]byte, time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loc, nil)
	if err != nil {
		return nil, -1, err
	}
	req.Header.Set("User-Agent", o.agent)
	resp, err := o.client.Do(req)
	if err != nil {
		var redirect *redirectError
		var nerr net.Error
		if errors.As(err, &redirect) || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &nerr) && nerr.Timeout() {
			return nil, -1, err
		}
		return nil, 0, transient{err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("fetching %s: %s", loc, resp.Status)
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 && resp.StatusCode != http.StatusNotImplemented {
			return nil, retryAfter(resp.Header.Get("Retry-After"), time.Now()), transient{err}
		}
		return nil, -1, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchSize+1))
	if err != nil {
		return nil, 0, transient{fmt.Errorf("fetching %s: %w", loc, err)}
	}
	if len(body) > maxFetchSize {
		return nil, -1, fmt.Errorf("fetching %s: larger than %d MiB", loc, maxFetchSize>>20)
	}
	return body, 0, nil
}

// retryAfter reads a Retry-After header, given in seconds or as an HTTP
// date, as a wait of at least one second and at most maxRetryWait. It is 0,
// meaning back off as usual, when the header is absent or unreadable.
func retryAfter(h string, now time.Time) time.Duration {
	h = strings.TrimSpace(h)
	var wait time.Duration
	if n, err := strconv.Atoi(h); err == nil {
		wait = time.Duration(n) * time.Second
	} else if at, err := http.ParseTime(h); err == nil {
		wait = at.Sub(now)
	} else {
		return 0
	}
	return min(max(wait, time.Second), maxRetryWait)
}

// redirectError is a redirect the client refused; trying again would be
// refused the same way.
type redirectError struct{ msg string }

func (e *redirectError) Error() string { return e.msg }

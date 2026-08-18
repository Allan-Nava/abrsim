package manifest

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LoadOptions govern how a ladder is fetched.
type LoadOptions struct {
	// Headers go on every request. Credentials belong here and nowhere else:
	// a flag value lands in shell history and in CI logs.
	Headers   map[string]string
	UserAgent string
	// Measure spends one HEAD per segment to read Content-Length instead of
	// deriving a size from the declared bitrate. It is off by default because
	// it is thousands of requests against somebody's CDN.
	Measure     bool
	Concurrency int // parallel HEAD requests, default 8
	Timeout     time.Duration
	Client      *http.Client
	// MaxBytes caps one manifest body. A playlist is text; anything larger
	// than this is not one, and parsing it would be a way to be handed an
	// arbitrary amount of memory.
	MaxBytes int64
}

const defaultMaxBytes = 32 << 20

func (o LoadOptions) client() *http.Client {
	if o.Client != nil {
		return o.Client
	}
	t := o.Timeout
	if t <= 0 {
		t = 30 * time.Second
	}
	return &http.Client{Timeout: t}
}

func (o LoadOptions) agent() string {
	if o.UserAgent != "" {
		return o.UserAgent
	}
	return "abrsim"
}

// Load reads a ladder from a master playlist URL or a local file.
func Load(ctx context.Context, src string, o LoadOptions) (Ladder, error) {
	base, err := sourceURL(src)
	if err != nil {
		return Ladder{}, err
	}

	body, err := get(ctx, base, o)
	if err != nil {
		return Ladder{}, err
	}

	l, err := ParseMaster(body, base)
	if err != nil {
		// A media playlist is a legitimate thing to be handed and a useless
		// thing to simulate: one rung is not something a player chooses
		// between, so the error has to say what to point at instead rather
		// than complain about the syntax.
		if _, mediaErr := ParseMedia(body, base); mediaErr == nil {
			return Ladder{}, fmt.Errorf("%s is a media playlist, not a master: abrsim needs the master playlist, because a ladder of one rung has no adaptation to simulate", src)
		}
		return Ladder{}, fmt.Errorf("%s: %w", src, err)
	}

	for i := range l.Renditions {
		u, err := url.Parse(l.Renditions[i].URI)
		if err != nil {
			return Ladder{}, fmt.Errorf("%s: %w", l.Renditions[i].Name, err)
		}
		data, err := get(ctx, u, o)
		if err != nil {
			return Ladder{}, fmt.Errorf("%s: %w", l.Renditions[i].Name, err)
		}
		p, err := ParseMedia(data, u)
		if err != nil {
			return Ladder{}, fmt.Errorf("%s: %w", l.Renditions[i].Name, err)
		}
		l.Renditions[i].Segments = p.Segments
		l.Renditions[i].InitURI = p.InitURI
		l.Renditions[i].InitBytes = p.InitBytes
		l.Renditions[i].InitMeasured = p.InitMeasured
	}

	if o.Measure {
		measure(ctx, l.Renditions, o)
	}
	// Anything still without a size — never measured, or measured and refused —
	// falls back to the declared bitrate and keeps Measured false. A limit of
	// this tool is not a defect in the stream.
	for i := range l.Renditions {
		FillDeclaredSizes(&l.Renditions[i])
	}
	return l, nil
}

// sourceURL turns a CLI argument into a URL, accepting a local path.
func sourceURL(src string) (*url.URL, error) {
	u, err := url.Parse(src)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		return u, nil
	}
	abs, err := filepath.Abs(src)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", src, err)
	}
	return &url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}, nil
}

func get(ctx context.Context, u *url.URL, o LoadOptions) ([]byte, error) {
	if u.Scheme == "file" {
		return os.ReadFile(filepath.FromSlash(u.Path))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", o.agent())
	for k, v := range o.Headers {
		req.Header.Set(k, v)
	}

	resp, err := o.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d %s", u, resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	max := o.MaxBytes
	if max <= 0 {
		max = defaultMaxBytes
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", u, err)
	}
	if int64(len(body)) > max {
		return nil, fmt.Errorf("%s: body exceeds %d bytes, which is not a playlist", u, max)
	}
	return body, nil
}

// measure fills in real segment sizes from Content-Length.
//
// Every worker writes to a distinct segment, so the slice needs no lock — but
// the tests run under -race precisely because that is the kind of claim which
// stops being true when somebody adds a counter.
func measure(ctx context.Context, rs []Rendition, o LoadOptions) {
	type job struct{ r, s int }

	jobs := make(chan job)
	n := o.Concurrency
	if n <= 0 {
		n = 8
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				seg := &rs[j.r].Segments[j.s]
				if seg.Measured {
					continue
				}
				if size, ok := head(ctx, seg.URI, o); ok {
					seg.Bytes, seg.Measured = size, true
				}
			}
		}()
	}
	for r := range rs {
		for s := range rs[r].Segments {
			jobs <- job{r, s}
		}
	}
	close(jobs)
	wg.Wait()
}

// head reads a Content-Length, reporting (0, false) for anything it could not
// read — a redirect that drops the header, a 4xx, a server that refuses HEAD.
// The caller then keeps the declared estimate, which is the honest answer.
func head(ctx context.Context, raw string, o LoadOptions) (int64, bool) {
	if strings.HasPrefix(raw, "file:") {
		if st, err := os.Stat(strings.TrimPrefix(raw, "file://")); err == nil {
			return st.Size(), true
		}
		return 0, false
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, raw, nil)
	if err != nil {
		return 0, false
	}
	req.Header.Set("User-Agent", o.agent())
	for k, v := range o.Headers {
		req.Header.Set(k, v)
	}

	resp, err := o.client().Do(req)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.ContentLength <= 0 {
		return 0, false
	}
	return resp.ContentLength, true
}

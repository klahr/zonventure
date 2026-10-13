package updates

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/klahr/zonventure/internal/logx"
	"github.com/klahr/zonventure/internal/mdx"
)

// DefaultChangelogURL is the canonical fetch target for the upstream
// project. Operators of forks point at their own fork via the
// ZV_CHANGELOG_URL env var.
const DefaultChangelogURL = "https://raw.githubusercontent.com/klahr/zonventure/main/CHANGELOG.md"

// Off as the changelog URL turns update checks off: nothing is fetched.
// cmd/server passes it when ZV_CHANGELOG_URL is set but empty.
const Off = "off"

// DefaultPollInterval is how often the background loop refetches
// the changelog. Daily is plenty — releases are weekly at best, and
// admins can hit the "Check now" button if they want it sooner.
const DefaultPollInterval = 24 * time.Hour

// maxBodyBytes caps the response body read from the changelog URL.
// CHANGELOG.md should be tens of KB tops; 1 MiB is the well-padded
// abuse ceiling.
const maxBodyBytes int64 = 1 << 20

// Section is one entry in the API response. Mirrors Release but
// pre-renders notes to HTML so the client doesn't ship a markdown
// renderer just for this surface.
type Section struct {
	Version   string `json:"version"`
	Date      string `json:"date,omitempty"`
	NotesHTML string `json:"notes_html,omitempty"`
}

// Status is the snapshot served via the admin API. Fields are stable —
// the UI keys off them, and a future "what's new" widget on the
// dashboard would too.
type Status struct {
	// Current is the running binary's version, normalised (the
	// MAJOR.MINOR.PATCH portion of whatever main.version held).
	// Empty when the binary couldn't be parsed (e.g. "dev").
	Current string `json:"current"`
	// CurrentRaw is the unparsed version string. Useful for the UI
	// to differentiate between "0.1.0" and "0.1.0-3-gabcd" without
	// losing the trailer.
	CurrentRaw string `json:"current_raw"`
	// IsDevBuild is true when the binary reports anything other than
	// a clean tag — `git describe` past a tag, "dev", "-dirty", etc.
	// The UI uses this to show "you're on a development build"
	// instead of an "update available" prompt.
	IsDevBuild bool `json:"is_dev_build"`
	// Latest is the highest version found in the parsed changelog.
	// Empty when the feed is unavailable or empty.
	Latest string `json:"latest,omitempty"`
	// LatestDate is the YYYY-MM-DD release date of Latest, when the
	// changelog included one. Empty otherwise.
	LatestDate string `json:"latest_date,omitempty"`
	// UpdateAvailable is true when Current is parseable, not a dev
	// build, and Latest > Current. Stays false in every ambiguous
	// case so the UI's "update available!" affordance is always a
	// confident signal.
	UpdateAvailable bool `json:"update_available"`
	// Sections lists every changelog entry strictly newer than
	// Current, newest-first. Empty when no update is available.
	Sections []Section `json:"sections,omitempty"`
	// CheckedAt is when the cached snapshot was last refreshed.
	// Zero value means the service hasn't fetched yet (the UI should
	// show "checking…" rather than reporting a stale-looking time).
	CheckedAt time.Time `json:"checked_at,omitempty"`
	// FetchError carries the most recent fetch failure, if any.
	// Empty on success. The UI surfaces it as a small "couldn't
	// check" hint so operators know whether they're looking at a
	// stale-but-believable snapshot or a confused one.
	FetchError string `json:"fetch_error,omitempty"`
	// Disabled is true when update checks are turned off (an empty
	// ZV_CHANGELOG_URL); nothing is ever fetched.
	Disabled bool `json:"disabled,omitempty"`
}

// Service polls the changelog URL on a fixed interval, caches the
// last successfully parsed snapshot, and answers Status requests
// from the admin handler. Safe for concurrent use.
type Service struct {
	url      string
	current  string
	parsed   Semver
	parsedOK bool
	client   *http.Client
	interval time.Duration

	mu     sync.Mutex
	last   time.Time
	etag   string
	body   string
	status Status
}

// Config gathers the inputs to NewService so the call site (the
// server constructor) can stay readable as new knobs are added.
type Config struct {
	Version      string
	ChangelogURL string
	PollInterval time.Duration
	HTTPClient   *http.Client
}

// NewService builds a Service ready to be Start()ed. Defaults fill in
// for any zero-valued field — passing a bare Config{Version: v} is
// the common case.
func NewService(cfg Config) *Service {
	url := cfg.ChangelogURL
	if url == "" {
		url = DefaultChangelogURL
	}
	interval := cfg.PollInterval
	if interval <= 0 {
		interval = DefaultPollInterval
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	parsed, ok := ParseSemver(cfg.Version)
	s := &Service{
		url:      url,
		current:  cfg.Version,
		parsed:   parsed,
		parsedOK: ok,
		client:   client,
		interval: interval,
	}
	s.status = Status{
		Current:    parsed.String(),
		CurrentRaw: cfg.Version,
		IsDevBuild: !ok || parsed.HasSuffix(),
		Disabled:   url == Off,
	}
	if !ok {
		s.status.Current = ""
	}
	return s
}

// Start spawns the background poll goroutine. The first fetch fires
// immediately so the UI doesn't have to wait an hour to see anything.
// Stops when ctx is done.
func (s *Service) Start(ctx context.Context) {
	if s.url == Off {
		return
	}
	go s.loop(ctx)
}

func (s *Service) loop(ctx context.Context) {
	defer logx.Guard("updates.loop")
	// First fetch: best-effort, errors logged via the cached status,
	// not fatal.
	_ = s.refresh(ctx)
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = s.refresh(ctx)
		}
	}
}

// CheckNow forces an immediate refresh. Returns the post-refresh
// Status so the admin UI gets fresh bytes back from a single round
// trip without needing a separate poll. Errors propagate to the
// caller AND are stashed in Status.FetchError so a subsequent GET
// reports the same condition.
func (s *Service) CheckNow(ctx context.Context) (Status, error) {
	err := s.refresh(ctx)
	return s.Status(), err
}

// Status returns the most recent cached snapshot. Cheap; safe to
// call on every admin request.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// refresh fetches the changelog, parses it, and rebuilds the
// cached Status. On a non-2xx response or a transport error the
// previous body is kept and only Status.FetchError is updated —
// "couldn't check today" should never wipe out yesterday's known-good
// release notes.
func (s *Service) refresh(ctx context.Context) error {
	if s.url == Off {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		s.recordFetchErr("build request: " + err.Error())
		return err
	}
	// Ask for plain text — sourcehut's blob URL serves raw bytes
	// when the Accept header asks for them and the rendered HTML
	// page when it doesn't. Operators pointing at a different host
	// can adjust via ZV_CHANGELOG_URL if their host disagrees.
	req.Header.Set("Accept", "text/plain, text/markdown, */*;q=0.1")
	req.Header.Set("User-Agent", "zv-update-checker/1")
	s.mu.Lock()
	if s.etag != "" {
		req.Header.Set("If-None-Match", s.etag)
	}
	prevBody := s.body
	s.mu.Unlock()

	resp, err := s.client.Do(req)
	if err != nil {
		s.recordFetchErr("fetch: " + err.Error())
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified && prevBody != "" {
		// Body unchanged. Just bump CheckedAt so the UI knows we
		// successfully checked.
		s.mu.Lock()
		s.last = time.Now().UTC()
		s.status.CheckedAt = s.last
		s.status.FetchError = ""
		s.mu.Unlock()
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		s.recordFetchErr("status " + resp.Status)
		return errors.New("status " + resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		s.recordFetchErr("read: " + err.Error())
		return err
	}
	if int64(len(body)) > maxBodyBytes {
		s.recordFetchErr("response too large")
		return errors.New("response too large")
	}
	s.applyBody(string(body), resp.Header.Get("ETag"))
	return nil
}

func (s *Service) applyBody(body, etag string) {
	cl := Parse(body)
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.body = body
	s.etag = etag
	s.last = now
	s.status.FetchError = ""
	s.status.CheckedAt = now
	s.status.Sections = nil
	s.status.Latest = ""
	s.status.LatestDate = ""
	s.status.UpdateAvailable = false
	if latest, ok := cl.Latest(); ok {
		s.status.Latest = latest.Version.String()
		if !latest.Date.IsZero() {
			s.status.LatestDate = latest.Date.Format("2006-01-02")
		}
	}
	if !s.parsedOK {
		// Can't compare; UI shows "running a development build" and
		// no update prompt. Still expose the latest so the page can
		// display the upstream release info.
		return
	}
	// Treat development builds (anything past a tag) as "ahead of"
	// the latest release. The upstream tag may include backports
	// the dev binary is missing, but we have no way to tell —
	// honest "you're on a dev build" beats a misleading update prompt.
	if s.parsed.HasSuffix() {
		return
	}
	newer := cl.SectionsNewerThan(s.parsed)
	if len(newer) == 0 {
		return
	}
	s.status.UpdateAvailable = true
	s.status.Sections = make([]Section, 0, len(newer))
	for _, r := range newer {
		sec := Section{
			Version:   r.Version.String(),
			NotesHTML: mdx.Render(r.NotesMD),
		}
		if !r.Date.IsZero() {
			sec.Date = r.Date.Format("2006-01-02")
		}
		s.status.Sections = append(s.status.Sections, sec)
	}
}

func (s *Service) recordFetchErr(msg string) {
	s.mu.Lock()
	s.status.FetchError = msg
	s.status.CheckedAt = time.Now().UTC()
	s.mu.Unlock()
}

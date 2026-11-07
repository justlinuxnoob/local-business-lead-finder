package main

// The worker does the actual Google Maps scraping. It runs as a child
// process of the app ("LeadFinder --worker ...") so that when a search ends
// or is stopped, the whole browser is shut down with it, cleanly and at once.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gosom/google-maps-scraper/deduper"
	"github.com/gosom/google-maps-scraper/exiter"
	"github.com/gosom/google-maps-scraper/gmaps"
	"github.com/gosom/scrapemate"
	memprovider "github.com/gosom/scrapemate/adapters/providers/memory"
	"github.com/gosom/scrapemate/scrapemateapp"
	"github.com/mxschmitt/playwright-go"
)

// Polite defaults.
const (
	maxPlaces         = 100 // never check more than 100 places per search
	politeDelayMin    = 1500 * time.Millisecond
	politeDelayJitter = 1500 * time.Millisecond
)

// eventPrefix marks the worker's messages to the app on stdout, so they can be
// told apart from anything the browser tooling prints.
const eventPrefix = "@@leadfinder "

// event is one message from the worker to the app.
type event struct {
	Type    string `json:"type"` // phase, found, checked, place, error
	Phase   string `json:"phase,omitempty"`
	Message string `json:"message,omitempty"`
	N       int    `json:"n,omitempty"`
	Place   *Lead  `json:"place,omitempty"`
	Key     string `json:"key,omitempty"`
}

type emitter struct {
	mu sync.Mutex
	w  *bufio.Writer
}

func (e *emitter) send(ev event) {
	b, _ := json.Marshal(ev)

	e.mu.Lock()
	defer e.mu.Unlock()

	_, _ = e.w.WriteString(eventPrefix)
	_, _ = e.w.Write(b)
	_ = e.w.WriteByte('\n')
	_ = e.w.Flush()
}

// runWorker scrapes one search and reports progress on stdout. It always
// exits the process when done (see the comment at the top of this file).
func runWorker(query, town string, howMany int) {
	em := &emitter{w: bufio.NewWriter(os.Stdout)}

	if err := scrape(em, query, town, howMany); err != nil {
		em.send(event{Type: "error", Message: err.Error()})
		os.Exit(1)
	}

	os.Exit(0)
}

func scrape(em *emitter, query, town string, howMany int) error {
	howMany = max(1, min(howMany, maxPlaces))

	if !browserInstalled() {
		em.send(event{Type: "phase", Phase: phaseSetup, Message: "First time setup: downloading (about 150 MB), only once."})
	} else {
		em.send(event{Type: "phase", Phase: phaseStarting, Message: "Starting the browser…"})
	}

	if err := playwright.Install(&playwright.RunOptions{Browsers: []string{"chromium"}, Stdout: os.Stderr, Stderr: os.Stderr}); err != nil {
		return fmt.Errorf("could not download the browser: %w", err)
	}

	em.send(event{Type: "phase", Phase: phaseSearch, Message: "Opening Google Maps…"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mon := &progressMonitor{Exiter: exiter.New(), em: em}
	mon.SetSeedCount(1)
	mon.SetCancelFunc(cancel)

	// Google shows roughly 5-7 places per scroll; scroll just enough.
	depth := howMany/5 + 2

	job := gmaps.NewGmapJob("", "en", query, depth, false, "", 0,
		gmaps.WithDeduper(&limitDeduper{inner: deduper.New(), limit: howMany}),
		gmaps.WithExitMonitor(mon),
	)

	app, err := newScraperApp(&leadWriter{em: em, town: town})
	if err != nil {
		return err
	}

	go mon.Run(ctx)

	errc := make(chan error, 1)

	go func() { errc <- app.Start(ctx, job) }()

	select {
	case err = <-errc:
		if ctx.Err() != nil {
			err = nil
		}
	case <-ctx.Done():
		// Every place has been checked. Give scrapemate a moment to hand
		// over the last result; the process exit then closes the browser.
		select {
		case <-errc:
		case <-time.After(5 * time.Second):
		}

		err = nil
	}

	return err
}

func newScraperApp(w scrapemate.ResultWriter) (*scrapemateapp.ScrapemateApp, error) {
	cfg, err := scrapemateapp.NewConfig(
		[]scrapemate.ResultWriter{w},
		// Polite defaults: one page at a time, no parallel mass scraping.
		scrapemateapp.WithConcurrency(1),
		scrapemateapp.WithJS(scrapemateapp.DisableImages()),
		scrapemateapp.WithExitOnInactivity(3*time.Minute),
		scrapemateapp.WithPageReuseLimit(2),
		scrapemateapp.WithBrowserReuseLimit(200),
		scrapemateapp.WithProvider(politeProvider{memprovider.New()}),
	)
	if err != nil {
		return nil, err
	}

	return scrapemateapp.NewScrapeMateApp(cfg)
}

// leadWriter receives every scraped place and reports the good ones.
type leadWriter struct {
	em   *emitter
	town string
}

// Run reads until scrapemate closes the channel. It must not stop early on
// ctx.Done: the exit monitor cancels the context just before the last place's
// result is sent, and scrapemate would block forever (and lose that lead).
func (w *leadWriter) Run(_ context.Context, in <-chan scrapemate.Result) error {
	for res := range in {
		e, ok := res.Data.(*gmaps.Entry)
		if !ok || e == nil || !isLead(e) {
			continue
		}

		lead := toLead(e, w.town)
		w.em.send(event{Type: "place", Place: &lead, Key: dedupeKey(e)})
	}

	return nil
}

// politeProvider hands jobs to the scraper one by one with a short, slightly
// random pause in between, like a person clicking through results.
type politeProvider struct {
	scrapemate.JobProvider
}

func (p politeProvider) Jobs(ctx context.Context) (<-chan scrapemate.IJob, <-chan error) {
	innerJobs, errc := p.JobProvider.Jobs(ctx)
	out := make(chan scrapemate.IJob)

	// out is never closed: scrapemate's worker would read nil jobs from a
	// closed channel. It stops on ctx.Done instead.
	go func() {
		for job := range innerJobs {
			pause := politeDelayMin + time.Duration(rand.Int64N(int64(politeDelayJitter)))

			select {
			case <-ctx.Done():
				return
			case <-time.After(pause):
			}

			select {
			case <-ctx.Done():
				return
			case out <- job:
			}
		}
	}()

	return out, errc
}

// progressMonitor wraps gosom's exit monitor so we can show progress.
type progressMonitor struct {
	exiter.Exiter
	em *emitter
}

func (m *progressMonitor) IncrPlacesFound(n int) {
	m.Exiter.IncrPlacesFound(n)
	m.em.send(event{Type: "found", N: n})
}

func (m *progressMonitor) IncrPlacesCompleted(n int) {
	m.em.send(event{Type: "checked", N: n})
	m.Exiter.IncrPlacesCompleted(n)
}

// limitDeduper drops duplicates and stops queueing places after the limit.
type limitDeduper struct {
	mu    sync.Mutex
	inner deduper.Deduper
	limit int
	count int
}

func (d *limitDeduper) AddIfNotExists(ctx context.Context, key string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.count >= d.limit {
		return false
	}

	if !d.inner.AddIfNotExists(ctx, key) {
		return false
	}

	d.count++

	return true
}

// browserInstalled guesses whether Playwright's Chromium is already on disk,
// so we can warn about the one-time download.
func browserInstalled() bool {
	cache, err := os.UserCacheDir()
	if err != nil {
		return false
	}

	if _, err := os.Stat(filepath.Join(cache, "ms-playwright-go")); err != nil {
		return false
	}

	matches, _ := filepath.Glob(filepath.Join(cache, "ms-playwright", "chromium*"))

	return len(matches) > 0
}

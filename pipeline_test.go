package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gosom/google-maps-scraper/gmaps"
	"github.com/gosom/scrapemate"
)

// TestPipeline sends scraped places through the same path a real search
// uses: settings as passed to the worker, the worker's lead filter and
// events, the app reading those events, and the downloaded sheet.
func TestPipeline(t *testing.T) {
	st := defaultSettings()
	st.Columns.Hours = true

	// The app passes settings to the worker as a JSON command-line flag.
	raw, _ := json.Marshal(st)

	workerSettings, err := parseSettings(raw)
	if err != nil {
		t.Fatal(err)
	}

	places := []*gmaps.Entry{
		{Title: "No Site Barbers", PlaceID: "1", ReviewRating: 4.5, ReviewCount: 30},
		{Title: "FB Barbers", PlaceID: "2", ReviewRating: 4.8, ReviewCount: 90, WebSite: "https://www.facebook.com/fbbarbers",
			OpenHours: map[string][]string{"Monday": {"9 AM–6 PM"}}},
		{Title: "Real Site Barbers", PlaceID: "3", ReviewRating: 4.9, ReviewCount: 200, WebSite: "https://realsite.co.uk"},
		{Title: "Booksy Barbers", PlaceID: "4", ReviewRating: 4.1, ReviewCount: 12, WebSite: "https://booksy.com/en-gb/4"},
		{Title: "Bad Barbers", PlaceID: "5", ReviewRating: 3.2, ReviewCount: 40, WebSite: "https://facebook.com/bad"},
		{Title: "FB Barbers", PlaceID: "2", ReviewRating: 4.8, ReviewCount: 90, WebSite: "https://www.facebook.com/fbbarbers"},
	}

	var out bytes.Buffer

	lw := &leadWriter{em: &emitter{w: bufio.NewWriter(&out)}, town: "Leeds", settings: workerSettings}
	in := make(chan scrapemate.Result, len(places))

	for _, p := range places {
		in <- scrapemate.Result{Data: p}
	}

	close(in)

	if err := lw.Run(context.Background(), in); err != nil {
		t.Fatal(err)
	}

	// The app reads the worker's events.
	s := newSearcher()
	s.settings = st
	s.seen = map[string]bool{}

	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		payload, ok := strings.CutPrefix(line, eventPrefix)
		if !ok {
			t.Fatalf("unexpected line %q", line)
		}

		var ev event
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			t.Fatal(err)
		}

		s.apply(ev)
	}

	data, _, err := s.CSV()
	if err != nil {
		t.Fatal(err)
	}

	rows, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte(utf8BOM)))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	got := map[string][]string{}
	for _, r := range rows[1:] {
		got[r[0]] = r
	}

	if len(rows) != 4 {
		t.Fatalf("want 3 leads + header, got %d rows: %v", len(rows), rows)
	}

	if got["Real Site Barbers"] != nil || got["Bad Barbers"] != nil {
		t.Fatalf("own website / low rating should be skipped: %v", rows)
	}

	fb := got["FB Barbers"]
	if fb == nil || fb[6] != "Facebook" || fb[7] != "https://www.facebook.com/fbbarbers" || fb[9] != "Monday: 9 AM–6 PM" {
		t.Fatalf("facebook lead wrong: %q (header %q)", fb, rows[0])
	}

	if r := got["No Site Barbers"]; r == nil || r[6] != "None" || r[7] != "" {
		t.Fatalf("no-website lead wrong: %q", r)
	}

	if r := got["Booksy Barbers"]; r == nil || r[6] != "Booksy" {
		t.Fatalf("booksy lead wrong: %q", r)
	}

	s.mu.Lock()
	word := s.leadWord()
	s.mu.Unlock()

	if word != "leads with no website of their own" {
		t.Fatalf("leadWord = %q", word)
	}
}

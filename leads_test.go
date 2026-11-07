package main

import (
	"bytes"
	"encoding/csv"
	"testing"

	"github.com/gosom/google-maps-scraper/gmaps"
)

func TestIsLead(t *testing.T) {
	good := gmaps.Entry{Title: "Kev's Barbers", ReviewRating: 4.6, ReviewCount: 52}

	cases := []struct {
		name string
		mod  func(e *gmaps.Entry)
		want bool
	}{
		{"good lead", func(*gmaps.Entry) {}, true},
		{"has own website", func(e *gmaps.Entry) { e.WebSite = "https://kevs.example" }, false},
		{"facebook page kept", func(e *gmaps.Entry) { e.WebSite = "https://www.facebook.com/kevsbarbers" }, true},
		{"instagram kept", func(e *gmaps.Entry) { e.WebSite = "instagram.com/kevs" }, true},
		{"booksy kept", func(e *gmaps.Entry) { e.WebSite = "https://booksy.com/en-gb/123_kevs" }, true},
		{"low rating", func(e *gmaps.Entry) { e.ReviewRating = 3.9 }, false},
		{"exactly 4.0", func(e *gmaps.Entry) { e.ReviewRating = 4.0 }, true},
		{"few reviews", func(e *gmaps.Entry) { e.ReviewCount = 9 }, false},
		{"exactly 10 reviews", func(e *gmaps.Entry) { e.ReviewCount = 10 }, true},
		{"permanently closed", func(e *gmaps.Entry) { e.Status = "Permanently closed" }, false},
		{"closed enum", func(e *gmaps.Entry) { e.Status = "CLOSED" }, false},
		{"temporarily closed kept", func(e *gmaps.Entry) { e.Status = "Temporarily closed" }, true},
		{"no name", func(e *gmaps.Entry) { e.Title = " " }, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := good
			tc.mod(&e)

			if got := isLead(&e, defaultSettings()); got != tc.want {
				t.Fatalf("isLead = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBuildCSV(t *testing.T) {
	data, err := buildCSV([]Lead{{
		Presence: "Facebook", PageLink: "https://facebook.com/x",
		Name: "=HYPERLINK(\"x\")", Category: "Barber shop", Town: "Leeds",
		Phone: "+44 113 496 0000", Rating: 4.65, Reviews: 120, MapsLink: "https://maps.google.com/x",
	}}, defaultSettings().Columns)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.HasPrefix(data, []byte(utf8BOM)) {
		t.Fatal("missing UTF-8 BOM")
	}

	rows, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte(utf8BOM)))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"Name", "Category", "Town", "Phone", "Rating", "Reviews",
		"Has instead of a website", "Their page", "Google Maps link", "Status"}
	if len(rows) != 2 || len(rows[0]) != len(want) {
		t.Fatalf("unexpected rows: %v", rows)
	}

	for i := range want {
		if rows[0][i] != want[i] {
			t.Fatalf("header[%d] = %q, want %q", i, rows[0][i], want[i])
		}
	}

	r := rows[1]
	if r[0] != "'=HYPERLINK(\"x\")" {
		t.Errorf("formula not neutralised: %q", r[0])
	}

	if r[3] != " +44 113 496 0000" || r[4] != "4.7" || r[5] != "120" ||
		r[6] != "Facebook" || r[7] != "https://facebook.com/x" || r[9] != "" {
		t.Errorf("unexpected row: %q", r)
	}
}

func TestDedupeKey(t *testing.T) {
	a := gmaps.Entry{Title: "A", PlaceID: "p1"}
	b := gmaps.Entry{Title: "A different name", PlaceID: "p1"}

	if dedupeKey(&a) != dedupeKey(&b) {
		t.Fatal("same place id should dedupe")
	}

	c := gmaps.Entry{Title: "Shop", Phone: "0113 496 0000"}
	d := gmaps.Entry{Title: "shop ", Phone: "0113-496-0000"}

	if dedupeKey(&c) != dedupeKey(&d) {
		t.Fatal("same name and phone should dedupe")
	}
}

func TestFileNameFor(t *testing.T) {
	if got := fileNameFor("Barber Shop", "Leeds"); got != "leads-barber-shop-leeds.csv" {
		t.Fatalf("got %q", got)
	}
}

func TestCleanMapsLink(t *testing.T) {
	in := "https://www.google.com/maps/place/Modern+Barbers/data=!4m7!3m6?authuser=0&hl=en"
	if got := cleanMapsLink(in); got != "https://www.google.com/maps/place/Modern+Barbers/data=!4m7!3m6" {
		t.Fatalf("got %q", got)
	}
}

func TestClassifyWebsite(t *testing.T) {
	cases := []struct {
		in, kind, name string
	}{
		{"", kindNone, "None"},
		{"  ", kindNone, "None"},
		{"https://www.facebook.com/kevsbarbers", kindFacebook, "Facebook"},
		{"http://m.facebook.com/profile.php?id=1", kindFacebook, "Facebook"},
		{"https://business.facebook.com/x", kindFacebook, "Facebook"},
		{"facebook.com/kevs", kindFacebook, "Facebook"},
		{"https://FB.me/kevs", kindFacebook, "Facebook"},
		{"https://www.instagram.com/kevs/", kindInstagram, "Instagram"},
		{"https://www.tiktok.com/@kevs", kindSocial, "TikTok"},
		{"https://wa.me/447700900000", kindSocial, "WhatsApp"},
		{"https://linktr.ee/kevs", kindLinkPage, "Linktree"},
		{"https://kevs-barbers.business.site/", kindLinkPage, "Google business site"},
		{"https://maps.app.goo.gl/abc", kindLinkPage, "Google Maps link"},
		{"https://www.google.com/maps/place/x", kindLinkPage, "Google Maps link"},
		{"https://www.google.co.uk/maps?cid=1", kindLinkPage, "Google Maps link"},
		{"https://booksy.com/en-gb/123", kindBooking, "Booksy"},
		{"https://www.fresha.com/a/kevs", kindBooking, "Fresha"},
		{"https://www.ubereats.com/gb/store/x", kindDelivery, "Uber Eats"},
		{"https://food.bolt.eu/en/x", kindDelivery, "Bolt Food"},
		{"https://kevsbarbers.co.uk", kindOwn, "Own website"},
		{"https://kevs.wixsite.com/barbers", kindOwn, "Own website"},
		{"https://sites.google.com/view/kevs", kindOwn, "Own website"},
		{"https://notfacebook.com", kindOwn, "Own website"},
		{"https://facebook.com.evil.example", kindOwn, "Own website"},
	}

	for _, tc := range cases {
		kind, name := classifyWebsite(tc.in)
		if kind != tc.kind || name != tc.name {
			t.Errorf("classifyWebsite(%q) = %q, %q; want %q, %q", tc.in, kind, name, tc.kind, tc.name)
		}
	}
}

func TestSettingsFilter(t *testing.T) {
	fb := gmaps.Entry{Title: "A", ReviewRating: 4.2, ReviewCount: 20, WebSite: "https://facebook.com/a"}
	none := gmaps.Entry{Title: "B", ReviewRating: 4.2, ReviewCount: 20}

	st := defaultSettings()
	st.Include.Facebook = false

	if isLead(&fb, st) {
		t.Error("facebook should be skipped when turned off")
	}

	if !isLead(&none, st) {
		t.Error("no website should still be kept")
	}

	st = defaultSettings()
	st.MinRating, st.MinReviews = 4.5, 50

	if isLead(&none, st) {
		t.Error("rating/review minimums not applied")
	}

	st = defaultSettings()
	st.SkipTempClosed = true
	tmp := none
	tmp.Status = "Temporarily closed"

	if isLead(&tmp, st) {
		t.Error("temporarily closed should be skipped")
	}
}

func TestParseSettings(t *testing.T) {
	st, err := parseSettings(nil)
	if err != nil || st != defaultSettings() {
		t.Fatalf("empty settings should be defaults, got %+v, %v", st, err)
	}

	// Missing fields keep defaults; values are clamped.
	st, err = parseSettings([]byte(`{"minRating": 9, "minReviews": -3, "columns": {"hours": true}}`))
	if err != nil {
		t.Fatal(err)
	}

	if st.MinRating != 5 || st.MinReviews != 0 || !st.Include.Facebook || !st.Columns.Hours || !st.Columns.Presence {
		t.Fatalf("unexpected settings: %+v", st)
	}

	if _, err := parseSettings([]byte(`{"include": {"noWebsite": false, "facebook": false, "instagram": false,
		"otherSocial": false, "linkPages": false, "booking": false, "delivery": false}}`)); err == nil {
		t.Fatal("expected an error when nothing is included")
	}

	if _, err := parseSettings([]byte(`{nope`)); err == nil {
		t.Fatal("expected an error for bad JSON")
	}
}

func TestOptionalColumns(t *testing.T) {
	c := Columns{Address: true, Hours: true, Coordinates: true}

	var got []string
	for _, col := range sheetColumns(c) {
		got = append(got, col.title)
	}

	want := []string{"Name", "Category", "Town", "Phone", "Rating", "Reviews", "Google Maps link",
		"Address", "Opening hours", "Latitude", "Longitude", "Status"}

	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}

	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestFormatHours(t *testing.T) {
	got := formatHours(map[string][]string{
		"Tuesday": {"9 AM–5 PM"},
		"Monday":  {"Closed"},
		"Sunday":  {"10 AM–1 PM", "2–4 PM"},
	})

	want := "Monday: Closed; Tuesday: 9 AM–5 PM; Sunday: 10 AM–1 PM, 2–4 PM"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

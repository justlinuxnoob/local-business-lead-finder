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
		{"has website", func(e *gmaps.Entry) { e.WebSite = "https://kevs.example" }, false},
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

			if got := isLead(&e); got != tc.want {
				t.Fatalf("isLead = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBuildCSV(t *testing.T) {
	data, err := buildCSV([]Lead{{
		Name: "=HYPERLINK(\"x\")", Category: "Barber shop", Town: "Leeds",
		Phone: "+44 113 496 0000", Rating: 4.65, Reviews: 120, MapsLink: "https://maps.google.com/x",
	}})
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

	want := []string{"Name", "Category", "Town", "Phone", "Rating", "Reviews", "Google Maps link", "Status"}
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

	if r[3] != " +44 113 496 0000" || r[4] != "4.7" || r[5] != "120" || r[7] != "" {
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

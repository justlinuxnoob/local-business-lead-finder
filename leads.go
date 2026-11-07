package main

import (
	"bytes"
	"encoding/csv"
	"strconv"
	"strings"

	"github.com/gosom/google-maps-scraper/gmaps"
)

// Lead filter rules. These are deliberately fixed so the app stays simple.
const (
	minRating  = 4.0
	minReviews = 10
)

// utf8BOM makes Excel open the CSV as UTF-8 so accents show correctly.
const utf8BOM = "\xEF\xBB\xBF"

// csvHeader is the exact column order of the exported sheet.
var csvHeader = []string{"Name", "Category", "Town", "Phone", "Rating", "Reviews", "Google Maps link", "Status"}

// Lead is one row of the exported sheet.
type Lead struct {
	Name     string
	Category string
	Town     string
	Phone    string
	Rating   float64
	Reviews  int
	MapsLink string
}

// isLead reports whether a scraped place is a good prospect:
// no website, rating 4.0+, 10+ reviews and not permanently closed.
func isLead(e *gmaps.Entry) bool {
	if e == nil || strings.TrimSpace(e.Title) == "" {
		return false
	}

	if strings.TrimSpace(e.WebSite) != "" {
		return false
	}

	if e.ReviewRating < minRating || e.ReviewCount < minReviews {
		return false
	}

	return !isPermanentlyClosed(e.Status)
}

func isPermanentlyClosed(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))

	// Google Maps reports either a human text ("Permanently closed") or an
	// enum ("CLOSED") for places that shut down for good.
	return strings.Contains(s, "permanently") || s == "closed"
}

// toLead converts a scraped place into a sheet row. fallbackTown is used when
// Google does not return a structured town for the place.
func toLead(e *gmaps.Entry, fallbackTown string) Lead {
	town := strings.TrimSpace(e.CompleteAddress.City)
	if town == "" {
		town = fallbackTown
	}

	category := strings.TrimSpace(e.Category)
	if category == "" && len(e.Categories) > 0 {
		category = e.Categories[0]
	}

	return Lead{
		Name:     strings.TrimSpace(e.Title),
		Category: category,
		Town:     town,
		Phone:    strings.TrimSpace(e.Phone),
		Rating:   e.ReviewRating,
		Reviews:  e.ReviewCount,
		MapsLink: cleanMapsLink(e.Link),
	}
}

// cleanMapsLink drops Google's tracking parameters (?authuser=…&g_ep=…);
// the remaining place URL opens the same listing.
func cleanMapsLink(link string) string {
	if i := strings.IndexByte(link, '?'); i > 0 {
		return link[:i]
	}

	return link
}

// dedupeKey identifies a place so the same business is never listed twice.
func dedupeKey(e *gmaps.Entry) string {
	switch {
	case e.PlaceID != "":
		return "pid:" + e.PlaceID
	case e.Cid != "":
		return "cid:" + e.Cid
	case e.DataID != "":
		return "did:" + e.DataID
	default:
		return "np:" + strings.ToLower(strings.TrimSpace(e.Title)) + "|" + normalizePhone(e.Phone)
	}
}

func normalizePhone(p string) string {
	var b strings.Builder

	for _, r := range p {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}

	return b.String()
}

// buildCSV renders leads as a CSV that opens cleanly in Excel and Google Sheets.
// It starts with a UTF-8 byte order mark so Excel shows accents correctly.
func buildCSV(leads []Lead) ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString(utf8BOM)

	w := csv.NewWriter(&buf)
	w.UseCRLF = true

	if err := w.Write(csvHeader); err != nil {
		return nil, err
	}

	for _, l := range leads {
		row := []string{
			safeCell(l.Name),
			safeCell(l.Category),
			safeCell(l.Town),
			phoneCell(l.Phone),
			strconv.FormatFloat(l.Rating, 'f', 1, 64),
			strconv.Itoa(l.Reviews),
			l.MapsLink,
			"",
		}

		if err := w.Write(row); err != nil {
			return nil, err
		}
	}

	w.Flush()

	return buf.Bytes(), w.Error()
}

// safeCell stops spreadsheet apps from treating text as a formula
// (CSV/formula injection).
func safeCell(s string) string {
	if s == "" {
		return s
	}

	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}

	return s
}

// phoneCell keeps phone numbers like "+44 113 ..." readable instead of
// letting the spreadsheet treat them as a formula. A leading space is
// invisible but makes Excel and Google Sheets keep the value as text.
func phoneCell(s string) string {
	if strings.HasPrefix(s, "+") {
		return " " + s
	}

	return safeCell(s)
}

package main

import (
	"bytes"
	"encoding/csv"
	"sort"
	"strconv"
	"strings"

	"github.com/gosom/google-maps-scraper/gmaps"
)

// utf8BOM makes Excel open the CSV as UTF-8 so accents show correctly.
const utf8BOM = "\xEF\xBB\xBF"

// Lead is one row of the exported sheet. Optional columns are always
// collected; the settings decide which ones go into the file.
type Lead struct {
	Name     string
	Category string
	Town     string
	Phone    string
	Rating   float64
	Reviews  int
	MapsLink string

	// What they use instead of a website ("None", "Facebook", "Booksy"…)
	// and the link to it.
	Presence string
	PageLink string

	Address     string
	Postcode    string
	Categories  string
	Hours       string
	PriceRange  string
	Description string
	Latitude    float64
	Longitude   float64
	PlusCode    string
}

// isLead reports whether a scraped place is a good prospect: no website of
// its own (only the kinds of page the settings allow), a good enough rating,
// enough reviews and not permanently closed.
func isLead(e *gmaps.Entry, st Settings) bool {
	if e == nil || strings.TrimSpace(e.Title) == "" {
		return false
	}

	if kind, _ := classifyWebsite(e.WebSite); !st.Include.allows(kind) {
		return false
	}

	if e.ReviewRating < st.MinRating || e.ReviewCount < st.MinReviews {
		return false
	}

	if st.SkipTempClosed && isTemporarilyClosed(e.Status) {
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

func isTemporarilyClosed(status string) bool {
	s := strings.ToLower(strings.TrimSpace(status))

	return strings.Contains(s, "temporarily") || s == "closed_temporarily"
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

	_, presence := classifyWebsite(e.WebSite)

	return Lead{
		Name:     strings.TrimSpace(e.Title),
		Category: category,
		Town:     town,
		Phone:    strings.TrimSpace(e.Phone),
		Rating:   e.ReviewRating,
		Reviews:  e.ReviewCount,
		MapsLink: cleanMapsLink(e.Link),

		Presence: presence,
		PageLink: strings.TrimSpace(e.WebSite),

		Address:     strings.TrimSpace(e.Address),
		Postcode:    strings.TrimSpace(e.CompleteAddress.PostalCode),
		Categories:  strings.Join(e.Categories, ", "),
		Hours:       formatHours(e.OpenHours),
		PriceRange:  strings.TrimSpace(e.PriceRange),
		Description: strings.TrimSpace(e.Description),
		Latitude:    e.Latitude,
		Longitude:   e.Longtitude,
		PlusCode:    strings.TrimSpace(e.PlusCode),
	}
}

var weekdays = []string{"Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday"}

// formatHours turns Google's opening hours into one readable line:
// "Monday: 9 AM–5 PM; Tuesday: 9 AM–5 PM; …".
func formatHours(h map[string][]string) string {
	if len(h) == 0 {
		return ""
	}

	var parts []string

	done := map[string]bool{}

	add := func(day string) {
		times, ok := h[day]
		if !ok || done[day] {
			return
		}

		done[day] = true
		parts = append(parts, day+": "+strings.Join(times, ", "))
	}

	for _, d := range weekdays {
		add(d)
	}

	// Anything not named in English (or spelled differently), in a stable order.
	rest := make([]string, 0, len(h))
	for d := range h {
		if !done[d] {
			rest = append(rest, d)
		}
	}

	sort.Strings(rest)

	for _, d := range rest {
		add(d)
	}

	return strings.Join(parts, "; ")
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

// sheetColumn is one column of the exported sheet.
type sheetColumn struct {
	title string
	value func(l Lead) string
}

// sheetColumns returns the columns to export, in order. "Status" is always
// last and empty: it is for the user to track who they have contacted.
func sheetColumns(c Columns) []sheetColumn {
	cols := []sheetColumn{
		{"Name", func(l Lead) string { return safeCell(l.Name) }},
		{"Category", func(l Lead) string { return safeCell(l.Category) }},
		{"Town", func(l Lead) string { return safeCell(l.Town) }},
		{"Phone", func(l Lead) string { return phoneCell(l.Phone) }},
		{"Rating", func(l Lead) string { return strconv.FormatFloat(l.Rating, 'f', 1, 64) }},
		{"Reviews", func(l Lead) string { return strconv.Itoa(l.Reviews) }},
	}

	if c.Presence {
		cols = append(cols,
			sheetColumn{"Has instead of a website", func(l Lead) string { return safeCell(l.Presence) }},
			sheetColumn{"Their page", func(l Lead) string { return safeCell(l.PageLink) }},
		)
	}

	cols = append(cols, sheetColumn{"Google Maps link", func(l Lead) string { return safeCell(l.MapsLink) }})

	optional := []struct {
		on  bool
		col sheetColumn
	}{
		{c.Address, sheetColumn{"Address", func(l Lead) string { return safeCell(l.Address) }}},
		{c.Postcode, sheetColumn{"Postcode", func(l Lead) string { return safeCell(l.Postcode) }}},
		{c.Categories, sheetColumn{"All categories", func(l Lead) string { return safeCell(l.Categories) }}},
		{c.Hours, sheetColumn{"Opening hours", func(l Lead) string { return safeCell(l.Hours) }}},
		{c.PriceRange, sheetColumn{"Price range", func(l Lead) string { return safeCell(l.PriceRange) }}},
		{c.Description, sheetColumn{"Description", func(l Lead) string { return safeCell(l.Description) }}},
		{c.Coordinates, sheetColumn{"Latitude", func(l Lead) string { return coord(l.Latitude) }}},
		{c.Coordinates, sheetColumn{"Longitude", func(l Lead) string { return coord(l.Longitude) }}},
		{c.PlusCode, sheetColumn{"Plus code", func(l Lead) string { return safeCell(l.PlusCode) }}},
	}

	for _, o := range optional {
		if o.on {
			cols = append(cols, o.col)
		}
	}

	return append(cols, sheetColumn{"Status", func(Lead) string { return "" }})
}

func coord(f float64) string {
	if f == 0 {
		return ""
	}

	return strconv.FormatFloat(f, 'f', 6, 64)
}

// buildCSV renders leads as a CSV that opens cleanly in Excel and Google Sheets.
// It starts with a UTF-8 byte order mark so Excel shows accents correctly.
func buildCSV(leads []Lead, c Columns) ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString(utf8BOM)

	w := csv.NewWriter(&buf)
	w.UseCRLF = true

	cols := sheetColumns(c)

	header := make([]string, len(cols))
	for i, col := range cols {
		header[i] = col.title
	}

	if err := w.Write(header); err != nil {
		return nil, err
	}

	row := make([]string, len(cols))

	for _, l := range leads {
		for i, col := range cols {
			row[i] = col.value(l)
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

package main

import (
	"encoding/json"
	"errors"
	"math"
)

// Settings are the optional choices on the page. The defaults keep every
// business that has no website of its own (nothing listed, or only a
// Facebook page, Instagram, a booking app…) and skip real websites.
type Settings struct {
	Include Include `json:"include"`

	MinRating      float64 `json:"minRating"`
	MinReviews     int     `json:"minReviews"`
	SkipTempClosed bool    `json:"skipTempClosed"`

	Columns Columns `json:"columns"`
}

// Include says which kinds of web presence count as a lead.
type Include struct {
	NoWebsite   bool `json:"noWebsite"`
	Facebook    bool `json:"facebook"`
	Instagram   bool `json:"instagram"`
	OtherSocial bool `json:"otherSocial"`
	LinkPages   bool `json:"linkPages"`
	Booking     bool `json:"booking"`
	Delivery    bool `json:"delivery"`
}

// Columns are the optional columns of the sheet.
type Columns struct {
	Presence    bool `json:"presence"` // "Has instead" + "Their page"
	Address     bool `json:"address"`
	Postcode    bool `json:"postcode"`
	Categories  bool `json:"categories"`
	Hours       bool `json:"hours"`
	PriceRange  bool `json:"priceRange"`
	Description bool `json:"description"`
	Coordinates bool `json:"coordinates"`
	PlusCode    bool `json:"plusCode"`
}

func defaultSettings() Settings {
	return Settings{
		Include: Include{
			NoWebsite:   true,
			Facebook:    true,
			Instagram:   true,
			OtherSocial: true,
			LinkPages:   true,
			Booking:     true,
			Delivery:    true,
		},
		MinRating:  4.0,
		MinReviews: 10,
		Columns:    Columns{Presence: true},
	}
}

// parseSettings reads settings sent by the page. Anything missing keeps its
// default, so an older page or an empty body still works.
func parseSettings(raw json.RawMessage) (Settings, error) {
	st := defaultSettings()

	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &st); err != nil {
			return st, errors.New("the settings could not be read")
		}
	}

	return st.normalize()
}

func (s Settings) normalize() (Settings, error) {
	if math.IsNaN(s.MinRating) {
		s.MinRating = 0
	}

	s.MinRating = math.Round(min(5, max(0, s.MinRating))*10) / 10
	s.MinReviews = min(1_000_000, max(0, s.MinReviews))

	if !s.Include.any() {
		return s, errors.New("in Settings, tick at least one kind of business to include")
	}

	return s, nil
}

func (in Include) any() bool {
	return in.NoWebsite || in.Facebook || in.Instagram || in.OtherSocial ||
		in.LinkPages || in.Booking || in.Delivery
}

// allows reports whether a place with this kind of web presence is wanted.
func (in Include) allows(kind string) bool {
	switch kind {
	case kindNone:
		return in.NoWebsite
	case kindFacebook:
		return in.Facebook
	case kindInstagram:
		return in.Instagram
	case kindSocial:
		return in.OtherSocial
	case kindLinkPage:
		return in.LinkPages
	case kindBooking:
		return in.Booking
	case kindDelivery:
		return in.Delivery
	default:
		return false // own website: never a lead
	}
}

// onlyNoWebsite is true when the search keeps just places with nothing
// listed, to word the progress messages the way 1.0 did.
func (in Include) onlyNoWebsite() bool {
	return in == Include{NoWebsite: true}
}

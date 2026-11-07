package main

import (
	"net/url"
	"strings"
)

// Kinds of web presence a Google Maps listing can have. Anything that is not
// "own" means the business has no website of its own, so it is still a lead.
const (
	kindNone      = "none"      // nothing listed at all
	kindFacebook  = "facebook"  // a Facebook page
	kindInstagram = "instagram" // an Instagram profile
	kindSocial    = "social"    // other social media (TikTok, X, LinkedIn…)
	kindLinkPage  = "linkpage"  // link-in-bio pages and old free Google sites
	kindBooking   = "booking"   // booking apps and directory listings
	kindDelivery  = "delivery"  // food delivery apps
	kindOwn       = "own"       // a real website of their own
)

// platform is a site that is not the business's own website.
type platform struct {
	kind string
	name string
}

// platforms maps a domain (without "www.") to what it is. Subdomains match
// too, so "m.facebook.com" and "business.facebook.com" are both Facebook.
var platforms = map[string]platform{
	// Facebook
	"facebook.com":  {kindFacebook, "Facebook"},
	"fb.com":        {kindFacebook, "Facebook"},
	"fb.me":         {kindFacebook, "Facebook"},
	"fb.watch":      {kindFacebook, "Facebook"},
	"facebook.me":   {kindFacebook, "Facebook"},
	"messenger.com": {kindFacebook, "Facebook"},

	// Instagram
	"instagram.com": {kindInstagram, "Instagram"},
	"instagr.am":    {kindInstagram, "Instagram"},

	// Other social media
	"tiktok.com":     {kindSocial, "TikTok"},
	"x.com":          {kindSocial, "X (Twitter)"},
	"twitter.com":    {kindSocial, "X (Twitter)"},
	"linkedin.com":   {kindSocial, "LinkedIn"},
	"youtube.com":    {kindSocial, "YouTube"},
	"youtu.be":       {kindSocial, "YouTube"},
	"pinterest.com":  {kindSocial, "Pinterest"},
	"threads.net":    {kindSocial, "Threads"},
	"threads.com":    {kindSocial, "Threads"},
	"snapchat.com":   {kindSocial, "Snapchat"},
	"wa.me":          {kindSocial, "WhatsApp"},
	"whatsapp.com":   {kindSocial, "WhatsApp"},
	"t.me":           {kindSocial, "Telegram"},
	"telegram.me":    {kindSocial, "Telegram"},
	"vk.com":         {kindSocial, "VK"},
	"nextdoor.com":   {kindSocial, "Nextdoor"},
	"nextdoor.co.uk": {kindSocial, "Nextdoor"},

	// Link-in-bio pages and Google's old free sites (shut down in 2024)
	"linktr.ee":       {kindLinkPage, "Linktree"},
	"linktree.com":    {kindLinkPage, "Linktree"},
	"beacons.ai":      {kindLinkPage, "Beacons"},
	"bio.link":        {kindLinkPage, "bio.link"},
	"lnk.bio":         {kindLinkPage, "Lnk.Bio"},
	"taplink.cc":      {kindLinkPage, "Taplink"},
	"campsite.bio":    {kindLinkPage, "Campsite"},
	"solo.to":         {kindLinkPage, "solo.to"},
	"linkin.bio":      {kindLinkPage, "Linkin.bio"},
	"msha.ke":         {kindLinkPage, "Milkshake"},
	"business.site":   {kindLinkPage, "Google business site"},
	"g.page":          {kindLinkPage, "Google business page"},
	"maps.app.goo.gl": {kindLinkPage, "Google Maps link"},
	"goo.gl":          {kindLinkPage, "Google link"},

	// Booking apps and directories
	"booksy.com":           {kindBooking, "Booksy"},
	"fresha.com":           {kindBooking, "Fresha"},
	"treatwell.co.uk":      {kindBooking, "Treatwell"},
	"treatwell.com":        {kindBooking, "Treatwell"},
	"treatwell.de":         {kindBooking, "Treatwell"},
	"treatwell.fr":         {kindBooking, "Treatwell"},
	"treatwell.es":         {kindBooking, "Treatwell"},
	"treatwell.it":         {kindBooking, "Treatwell"},
	"treatwell.nl":         {kindBooking, "Treatwell"},
	"treatwell.lt":         {kindBooking, "Treatwell"},
	"vagaro.com":           {kindBooking, "Vagaro"},
	"styleseat.com":        {kindBooking, "StyleSeat"},
	"setmore.com":          {kindBooking, "Setmore"},
	"simplybook.me":        {kindBooking, "SimplyBook.me"},
	"simplybook.it":        {kindBooking, "SimplyBook.me"},
	"squareup.com":         {kindBooking, "Square Appointments"},
	"calendly.com":         {kindBooking, "Calendly"},
	"acuityscheduling.com": {kindBooking, "Acuity"},
	"as.me":                {kindBooking, "Acuity"},
	"schedulicity.com":     {kindBooking, "Schedulicity"},
	"mindbodyonline.com":   {kindBooking, "Mindbody"},
	"booker.com":           {kindBooking, "Booker"},
	"phorest.com":          {kindBooking, "Phorest"},
	"timely.com":           {kindBooking, "Timely"},
	"gettimely.com":        {kindBooking, "Timely"},
	"planity.com":          {kindBooking, "Planity"},
	"opentable.com":        {kindBooking, "OpenTable"},
	"opentable.co.uk":      {kindBooking, "OpenTable"},
	"resy.com":             {kindBooking, "Resy"},
	"thefork.com":          {kindBooking, "TheFork"},
	"thefork.co.uk":        {kindBooking, "TheFork"},
	"yelp.com":             {kindBooking, "Yelp"},
	"yelp.co.uk":           {kindBooking, "Yelp"},
	"tripadvisor.com":      {kindBooking, "Tripadvisor"},
	"tripadvisor.co.uk":    {kindBooking, "Tripadvisor"},
	"yell.com":             {kindBooking, "Yell"},
	"checkatrade.com":      {kindBooking, "Checkatrade"},
	"mybuilder.com":        {kindBooking, "MyBuilder"},
	"ratedpeople.com":      {kindBooking, "Rated People"},
	"trustatrade.com":      {kindBooking, "TrustATrader"},
	"trustatrader.com":     {kindBooking, "TrustATrader"},
	"bark.com":             {kindBooking, "Bark"},
	"houzz.com":            {kindBooking, "Houzz"},
	"houzz.co.uk":          {kindBooking, "Houzz"},
	"thumbtack.com":        {kindBooking, "Thumbtack"},
	"angi.com":             {kindBooking, "Angi"},
	"homeadvisor.com":      {kindBooking, "HomeAdvisor"},
	"bbb.org":              {kindBooking, "BBB"},
	"airbnb.com":           {kindBooking, "Airbnb"},
	"booking.com":          {kindBooking, "Booking.com"},

	// Food delivery apps
	"ubereats.com":      {kindDelivery, "Uber Eats"},
	"deliveroo.co.uk":   {kindDelivery, "Deliveroo"},
	"deliveroo.com":     {kindDelivery, "Deliveroo"},
	"deliveroo.ie":      {kindDelivery, "Deliveroo"},
	"deliveroo.fr":      {kindDelivery, "Deliveroo"},
	"deliveroo.it":      {kindDelivery, "Deliveroo"},
	"just-eat.co.uk":    {kindDelivery, "Just Eat"},
	"just-eat.com":      {kindDelivery, "Just Eat"},
	"just-eat.ie":       {kindDelivery, "Just Eat"},
	"justeat.com":       {kindDelivery, "Just Eat"},
	"doordash.com":      {kindDelivery, "DoorDash"},
	"grubhub.com":       {kindDelivery, "Grubhub"},
	"seamless.com":      {kindDelivery, "Seamless"},
	"postmates.com":     {kindDelivery, "Postmates"},
	"wolt.com":          {kindDelivery, "Wolt"},
	"glovoapp.com":      {kindDelivery, "Glovo"},
	"foodpanda.com":     {kindDelivery, "foodpanda"},
	"menulog.com.au":    {kindDelivery, "Menulog"},
	"skipthedishes.com": {kindDelivery, "SkipTheDishes"},
	"takeaway.com":      {kindDelivery, "Takeaway.com"},
	"lieferando.de":     {kindDelivery, "Lieferando"},
	"thuisbezorgd.nl":   {kindDelivery, "Thuisbezorgd"},
	"pyszne.pl":         {kindDelivery, "Pyszne.pl"},
	"food.bolt.eu":      {kindDelivery, "Bolt Food"},
}

// classifyWebsite works out what kind of link a listing has in its
// "website" field and returns the kind and a readable platform name.
func classifyWebsite(raw string) (kind, name string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return kindNone, "None"
	}

	host := hostOf(raw)
	if host == "" {
		return kindOwn, "Own website"
	}

	// Try the full host, then each parent domain: "m.facebook.com",
	// "facebook.com", "com".
	for h := host; h != ""; {
		if p, ok := platforms[h]; ok {
			return p.kind, p.name
		}

		i := strings.IndexByte(h, '.')
		if i < 0 {
			break
		}

		h = h[i+1:]
	}

	// A link back to Google Maps is not a website. (sites.google.com is a
	// real, if basic, website, so it stays "own".)
	if isGoogleMapsLink(host, raw) {
		return kindLinkPage, "Google Maps link"
	}

	return kindOwn, "Own website"
}

func isGoogleMapsLink(host, raw string) bool {
	switch host {
	case "maps.google.com", "business.google.com":
		return true
	case "google.com":
		u, err := url.Parse(withScheme(raw))
		return err == nil && strings.HasPrefix(u.Path, "/maps")
	}

	// Country domains such as google.co.uk/maps.
	if strings.HasPrefix(host, "google.") || strings.HasPrefix(host, "maps.google.") {
		u, err := url.Parse(withScheme(raw))
		return err == nil && (strings.HasPrefix(host, "maps.") || strings.HasPrefix(u.Path, "/maps"))
	}

	return false
}

// hostOf returns the lowercase host of a URL without "www.".
func hostOf(raw string) string {
	u, err := url.Parse(withScheme(raw))
	if err != nil {
		return ""
	}

	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))

	return strings.TrimPrefix(host, "www.")
}

func withScheme(raw string) string {
	if strings.Contains(raw, "://") {
		return raw
	}

	return "http://" + strings.TrimPrefix(raw, "//")
}

package listing

import (
	"testing"
	"time"
)

var now = time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

func sample(id string, price, area float64, opts ...func(*Listing)) Listing {
	lat, lon := 51.1, 71.4
	l := Listing{
		ID:        id,
		Price:     price,
		Area:      area,
		Lat:       &lat,
		Lon:       &lon,
		FirstSeen: now.AddDate(0, 0, -10),
	}
	for _, opt := range opts {
		opt(&l)
	}
	return l
}

func TestAnalyzeMeasuresEveryListingAgainstTheMedian(t *testing.T) {
	listings := []Listing{
		sample("cheap", 20_000_000, 50),  // 400k per m2
		sample("middle", 25_000_000, 50), // 500k
		sample("dear", 30_000_000, 50),   // 600k
	}

	views, stats := Analyze(listings, now)

	if stats.Median != 500_000 {
		t.Fatalf("median = %v, want 500000", stats.Median)
	}
	if stats.Count != 3 || stats.Mapped != 3 {
		t.Fatalf("stats = %+v, want 3 listings all mapped", stats)
	}
	if got := []string{views[0].ID, views[1].ID, views[2].ID}; got[0] != "cheap" || got[2] != "dear" {
		t.Fatalf("views are not sorted by price per m2: %v", got)
	}
	if views[0].Deviation != -20 || views[2].Deviation != 20 {
		t.Fatalf("deviations = %v and %v, want -20 and 20", views[0].Deviation, views[2].Deviation)
	}
}

func TestAnalyzeUsesTheEvenMedian(t *testing.T) {
	listings := []Listing{
		sample("a", 20_000_000, 50),
		sample("b", 30_000_000, 50),
	}

	_, stats := Analyze(listings, now)

	if stats.Median != 500_000 {
		t.Fatalf("median = %v, want the mean of the two middle values", stats.Median)
	}
}

func TestAnalyzeSkipsListingsWithoutPricePerM2(t *testing.T) {
	listings := []Listing{
		sample("ok", 20_000_000, 50),
		sample("no-area", 20_000_000, 0),
		sample("no-price", 0, 50),
	}

	views, stats := Analyze(listings, now)

	if len(views) != 1 || views[0].ID != "ok" {
		t.Fatalf("views = %+v, want only the usable listing", views)
	}
	if stats.Count != 1 {
		t.Fatalf("count = %d, want 1", stats.Count)
	}
}

func TestAnalyzeOfNothingIsEmpty(t *testing.T) {
	views, stats := Analyze(nil, now)

	if views != nil || stats != (Stats{}) {
		t.Fatalf("got %+v %+v, want empty results", views, stats)
	}
}

func TestViewCarriesPriceChangeAndAge(t *testing.T) {
	listings := []Listing{sample("a", 27_000_000, 50, func(l *Listing) {
		l.History = []PricePoint{
			{Date: now.AddDate(0, 0, -10), Price: 30_000_000},
			{Date: now.AddDate(0, 0, -1), Price: 27_000_000},
		}
	})}

	views, stats := Analyze(listings, now)

	if views[0].PriceDelta != -3_000_000 {
		t.Fatalf("delta = %v, want -3000000", views[0].PriceDelta)
	}
	if views[0].Days != 10 {
		t.Fatalf("days = %d, want 10", views[0].Days)
	}
	if stats.Cheaper != 1 {
		t.Fatalf("cheaper = %d, want 1", stats.Cheaper)
	}
}

func TestViewReportsListingsWithoutCoordinates(t *testing.T) {
	listings := []Listing{sample("a", 20_000_000, 50, func(l *Listing) {
		l.Lat, l.Lon = nil, nil
	})}

	views, stats := Analyze(listings, now)

	if views[0].Lat != 0 || views[0].Lon != 0 {
		t.Fatalf("coordinates = %v %v, want zeroes", views[0].Lat, views[0].Lon)
	}
	if stats.Mapped != 0 {
		t.Fatalf("mapped = %d, want 0", stats.Mapped)
	}
}

func TestStatsCountOwners(t *testing.T) {
	listings := []Listing{
		sample("a", 20_000_000, 50, func(l *Listing) { l.Seller = "owner" }),
		sample("b", 20_000_000, 50, func(l *Listing) { l.Seller = "agent" }),
	}

	_, stats := Analyze(listings, now)

	if stats.Owners != 1 {
		t.Fatalf("owners = %d, want 1", stats.Owners)
	}
}

func TestPricePerM2(t *testing.T) {
	if got := (Listing{Price: 30_000_000, Area: 60}).PricePerM2(); got != 500_000 {
		t.Fatalf("price per m2 = %v, want 500000", got)
	}
	if got := (Listing{Price: 30_000_000}).PricePerM2(); got != 0 {
		t.Fatalf("price per m2 without area = %v, want 0", got)
	}
}

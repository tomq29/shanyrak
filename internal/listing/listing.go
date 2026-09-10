// Package listing holds the domain types shared by the store and the HTTP API,
// and the price statistics the map is built on.
package listing

import (
	"math"
	"sort"
	"time"
)

type Listing struct {
	ID          string
	Title       string
	Price       float64
	Area        float64
	Rooms       int
	Floor       int
	TotalFloors int
	YearBuilt   int
	Address     string
	City        string
	District    string
	Complex     string
	ComplexID   string
	Seller      string
	OwnerName   string
	Photo       string
	Lat         *float64
	Lon         *float64
	Flags       string
	DatePosted  string
	Link        string
	Status      string
	FirstSeen   time.Time
	LastSeen    time.Time
	GoneAt      *time.Time
	History     []PricePoint
}

type PricePoint struct {
	Date  time.Time `json:"date"`
	Price float64   `json:"price"`
}

// PricePerM2 is zero when the listing lacks a price or an area.
func (l Listing) PricePerM2() float64 {
	if l.Price <= 0 || l.Area <= 0 {
		return 0
	}
	return l.Price / l.Area
}

// View is a listing enriched with the numbers the map needs. Field names are
// short because every one of them is repeated for thousands of points.
type View struct {
	ID          string       `json:"id"`
	Price       float64      `json:"price"`
	PricePerM2  float64      `json:"ppm"`
	Deviation   float64      `json:"dev"`
	PriceDelta  float64      `json:"delta"`
	Area        float64      `json:"area"`
	Rooms       int          `json:"rooms,omitempty"`
	Floor       int          `json:"floor,omitempty"`
	TotalFloors int          `json:"floors,omitempty"`
	YearBuilt   int          `json:"year,omitempty"`
	Address     string       `json:"address"`
	Complex     string       `json:"complex,omitempty"`
	Seller      string       `json:"seller"`
	OwnerName   string       `json:"owner,omitempty"`
	Photo       string       `json:"photo,omitempty"`
	Flags       string       `json:"flags,omitempty"`
	Days        int          `json:"days"`
	DatePosted  string       `json:"posted,omitempty"`
	Status      string       `json:"status,omitempty"`
	Gone        bool         `json:"gone,omitempty"`
	Lat         float64      `json:"lat"`
	Lon         float64      `json:"lon"`
	Link        string       `json:"link"`
	History     []PricePoint `json:"hist,omitempty"`
}

type Stats struct {
	Median  float64 `json:"median"`
	Count   int     `json:"count"`
	Mapped  int     `json:"mapped"`
	Cheaper int     `json:"cheaper"`
	Owners  int     `json:"owners"`
}

// Analyze turns listings into map points: it drops those without a price per
// square metre, then measures each one against the median of the whole set.
// The median is per search, because comparing cities makes no sense.
func Analyze(listings []Listing, now time.Time) ([]View, Stats) {
	usable := make([]Listing, 0, len(listings))
	prices := make([]float64, 0, len(listings))
	for _, l := range listings {
		if ppm := l.PricePerM2(); ppm > 0 {
			usable = append(usable, l)
			prices = append(prices, ppm)
		}
	}
	if len(usable) == 0 {
		return nil, Stats{}
	}

	median := medianOf(prices)
	views := make([]View, 0, len(usable))
	stats := Stats{Median: median, Count: len(usable)}

	for _, l := range usable {
		view := view(l, median, now)
		if view.Lat != 0 || view.Lon != 0 {
			stats.Mapped++
		}
		if view.PriceDelta < 0 {
			stats.Cheaper++
		}
		if l.Seller == "owner" {
			stats.Owners++
		}
		views = append(views, view)
	}

	sort.Slice(views, func(i, j int) bool { return views[i].PricePerM2 < views[j].PricePerM2 })
	return views, stats
}

func view(l Listing, median float64, now time.Time) View {
	ppm := l.PricePerM2()
	v := View{
		ID:          l.ID,
		Price:       l.Price,
		PricePerM2:  math.Round(ppm),
		Deviation:   round1((ppm/median - 1) * 100),
		Area:        l.Area,
		Rooms:       l.Rooms,
		Floor:       l.Floor,
		TotalFloors: l.TotalFloors,
		YearBuilt:   l.YearBuilt,
		Address:     l.Address,
		Complex:     l.Complex,
		Seller:      l.Seller,
		OwnerName:   l.OwnerName,
		Photo:       l.Photo,
		Flags:       l.Flags,
		DatePosted:  l.DatePosted,
		Status:      l.Status,
		Gone:        l.GoneAt != nil,
		Link:        l.Link,
		History:     l.History,
	}
	if l.Lat != nil && l.Lon != nil {
		v.Lat, v.Lon = *l.Lat, *l.Lon
	}
	if !l.FirstSeen.IsZero() {
		v.Days = int(now.Sub(l.FirstSeen).Hours() / 24)
	}
	if len(l.History) > 0 {
		v.PriceDelta = l.Price - l.History[0].Price
	}
	return v
}

func medianOf(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	middle := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[middle]
	}
	return (sorted[middle-1] + sorted[middle]) / 2
}

func round1(value float64) float64 {
	return math.Round(value*10) / 10
}

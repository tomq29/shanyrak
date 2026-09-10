package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func insert(t *testing.T, s *Store, id string, price float64, mutate ...string) {
	t.Helper()
	stamp := time.Now().Format(timestampLayout)
	_, err := s.db.Exec(`INSERT INTO ads
		(id, title, price, area, rooms, floor, total_floors, address, seller, lat, lon,
		 link, first_seen, last_seen)
		VALUES (?, '2-комн', ?, 50, 2, 3, 9, 'Абая 1', 'owner', 51.1, 71.4,
		        'https://krisha.kz/a/show/'||?, ?, ?)`, id, price, id, stamp, stamp)
	if err != nil {
		t.Fatalf("insert %s: %v", id, err)
	}
	for _, sql := range mutate {
		if _, err := s.db.Exec(sql); err != nil {
			t.Fatalf("mutate: %v", err)
		}
	}
}

func TestOpenCreatesAnEmptyDatabase(t *testing.T) {
	s := open(t)

	listings, err := s.Listings(context.Background(), false)
	if err != nil {
		t.Fatalf("listings: %v", err)
	}
	if len(listings) != 0 {
		t.Fatalf("listings = %d, want 0", len(listings))
	}
}

func TestListingsReadEveryColumn(t *testing.T) {
	s := open(t)
	insert(t, s, "1", 30_000_000)

	listings, err := s.Listings(context.Background(), false)
	if err != nil {
		t.Fatalf("listings: %v", err)
	}

	got := listings[0]
	if got.ID != "1" || got.Price != 30_000_000 || got.Area != 50 {
		t.Fatalf("listing = %+v", got)
	}
	if got.Rooms != 2 || got.Floor != 3 || got.TotalFloors != 9 {
		t.Fatalf("floors and rooms not read: %+v", got)
	}
	if got.Lat == nil || *got.Lat != 51.1 {
		t.Fatalf("coordinates not read: %+v", got.Lat)
	}
	if got.FirstSeen.IsZero() || got.LastSeen.IsZero() {
		t.Fatalf("timestamps not parsed: %+v", got)
	}
	if got.GoneAt != nil {
		t.Fatalf("gone_at = %v, want nil", got.GoneAt)
	}
}

func TestListingsHideDelistedUnlessAsked(t *testing.T) {
	s := open(t)
	insert(t, s, "1", 30_000_000)
	insert(t, s, "2", 20_000_000, `UPDATE ads SET gone_at='2026-09-01T10:00:00' WHERE id='2'`)

	active, err := s.Listings(context.Background(), false)
	if err != nil {
		t.Fatalf("listings: %v", err)
	}
	all, err := s.Listings(context.Background(), true)
	if err != nil {
		t.Fatalf("listings: %v", err)
	}

	if len(active) != 1 || active[0].ID != "1" {
		t.Fatalf("active = %+v, want only listing 1", active)
	}
	if len(all) != 2 {
		t.Fatalf("all = %d, want 2", len(all))
	}
	for _, l := range all {
		if l.ID == "2" && l.GoneAt == nil {
			t.Fatal("delisted listing has no gone_at")
		}
	}
}

func TestListingsCarryPriceHistoryInOrder(t *testing.T) {
	s := open(t)
	insert(t, s, "1", 27_000_000)
	if _, err := s.db.Exec(`INSERT INTO price_history (ad_id, seen_at, price) VALUES
		('1','2026-09-01T10:00:00',30000000), ('1','2026-09-05T10:00:00',27000000)`); err != nil {
		t.Fatalf("history: %v", err)
	}

	listings, _ := s.Listings(context.Background(), false)

	history := listings[0].History
	if len(history) != 2 || history[0].Price != 30_000_000 || history[1].Price != 27_000_000 {
		t.Fatalf("history = %+v", history)
	}
	if history[0].Date.IsZero() {
		t.Fatal("history date not parsed")
	}
}

func TestListingReturnsNotFound(t *testing.T) {
	s := open(t)

	_, err := s.Listing(context.Background(), "missing")

	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestSetStatusStoresAndReplaces(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	insert(t, s, "1", 30_000_000)

	if err := s.SetStatus(ctx, "1", "liked"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := s.SetStatus(ctx, "1", "viewed"); err != nil {
		t.Fatalf("replace: %v", err)
	}

	got, err := s.Listing(ctx, "1")
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if got.Status != "viewed" {
		t.Fatalf("status = %q, want viewed", got.Status)
	}
}

func TestSetStatusRejectsUnknownValues(t *testing.T) {
	s := open(t)
	insert(t, s, "1", 30_000_000)

	err := s.SetStatus(context.Background(), "1", "loved")

	if !errors.Is(err, ErrBadStatus) {
		t.Fatalf("err = %v, want ErrBadStatus", err)
	}
}

func TestStatusOfMissingListingIsNotFound(t *testing.T) {
	s := open(t)

	if err := s.SetStatus(context.Background(), "nope", "liked"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("set: err = %v, want ErrNotFound", err)
	}
	if err := s.ClearStatus(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("clear: err = %v, want ErrNotFound", err)
	}
}

func TestClearStatusRemovesTheMark(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	insert(t, s, "1", 30_000_000)
	s.SetStatus(ctx, "1", "liked")

	if err := s.ClearStatus(ctx, "1"); err != nil {
		t.Fatalf("clear: %v", err)
	}

	got, _ := s.Listing(ctx, "1")
	if got.Status != "" {
		t.Fatalf("status = %q, want empty", got.Status)
	}
}

func TestCounts(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	insert(t, s, "1", 30_000_000)
	insert(t, s, "2", 20_000_000, `UPDATE ads SET gone_at='2026-09-01T10:00:00', lat=NULL WHERE id='2'`)
	s.SetStatus(ctx, "1", "liked")

	counts, err := s.Counts(ctx)
	if err != nil {
		t.Fatalf("counts: %v", err)
	}

	want := Counts{Listings: 2, Active: 1, Delisted: 1, Mapped: 1, Marks: 1, Liked: 1}
	if counts != want {
		t.Fatalf("counts = %+v, want %+v", counts, want)
	}
}

func TestMetaRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := open(t)

	if value, err := s.Meta(ctx, "absent"); err != nil || value != "" {
		t.Fatalf("meta = %q, %v, want empty", value, err)
	}
	if err := s.SetMeta(ctx, "notified_at", "2026-09-11T00:00:00"); err != nil {
		t.Fatalf("set meta: %v", err)
	}
	if err := s.SetMeta(ctx, "notified_at", "2026-09-12T00:00:00"); err != nil {
		t.Fatalf("overwrite meta: %v", err)
	}

	value, err := s.Meta(ctx, "notified_at")
	if err != nil || value != "2026-09-12T00:00:00" {
		t.Fatalf("meta = %q, %v", value, err)
	}
}

// Package store reads the SQLite databases written by the scraper and stores
// the marks a viewer leaves on the map.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/tomq29/shanyrak/internal/listing"
	"github.com/tomq29/shanyrak/schema"
)

var (
	ErrNotFound     = errors.New("listing not found")
	ErrBadStatus    = errors.New("unknown status")
	ValidStatuses   = []string{"liked", "viewed", "disliked"}
	timestampLayout = "2006-01-02T15:04:05"
)

const adColumns = `a.id, a.title, a.price, a.area, a.rooms, a.floor, a.total_floors,
	a.year_built, a.address, a.city, a.district, a.complex, a.complex_id, a.seller,
	a.owner_name, a.photo, a.lat, a.lon, a.flags, a.date_posted, a.link,
	a.first_seen, a.last_seen, a.gone_at, s.status`

type Store struct {
	db *sql.DB
}

// Open connects to a scraper database, creating it when the scraper has not run
// yet so the server can start on an empty directory.
func Open(path string) (*Store, error) {
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	db.SetMaxOpenConns(4)

	if _, err := db.Exec(schema.SQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema to %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Listings(ctx context.Context, includeGone bool) ([]listing.Listing, error) {
	where := "WHERE a.gone_at IS NULL"
	if includeGone {
		where = ""
	}
	query := fmt.Sprintf(`SELECT %s FROM ads a
		LEFT JOIN statuses s ON s.ad_id = a.id
		%s ORDER BY a.id`, adColumns, where)

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("select listings: %w", err)
	}
	defer rows.Close()

	var listings []listing.Listing
	byID := map[string]int{}
	for rows.Next() {
		l, err := scanListing(rows)
		if err != nil {
			return nil, err
		}
		byID[l.ID] = len(listings)
		listings = append(listings, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("select listings: %w", err)
	}
	if err := s.attachHistory(ctx, listings, byID); err != nil {
		return nil, err
	}
	return listings, nil
}

func (s *Store) Listing(ctx context.Context, id string) (listing.Listing, error) {
	query := fmt.Sprintf(`SELECT %s FROM ads a
		LEFT JOIN statuses s ON s.ad_id = a.id WHERE a.id = ?`, adColumns)

	l, err := scanListing(s.db.QueryRowContext(ctx, query, id))
	if errors.Is(err, sql.ErrNoRows) {
		return listing.Listing{}, ErrNotFound
	}
	if err != nil {
		return listing.Listing{}, err
	}
	if err := s.attachHistory(ctx, []listing.Listing{l}, map[string]int{l.ID: 0}); err != nil {
		return listing.Listing{}, err
	}
	return l, nil
}

// attachHistory fills price history for every listing in one query, so a map
// with thousands of points still costs two round trips.
func (s *Store) attachHistory(ctx context.Context, listings []listing.Listing, byID map[string]int) error {
	if len(listings) == 0 {
		return nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT ad_id, seen_at, price FROM price_history ORDER BY id`)
	if err != nil {
		return fmt.Errorf("select price history: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var adID, seenAt string
		var price float64
		if err := rows.Scan(&adID, &seenAt, &price); err != nil {
			return fmt.Errorf("scan price history: %w", err)
		}
		index, ok := byID[adID]
		if !ok {
			continue
		}
		listings[index].History = append(listings[index].History, listing.PricePoint{
			Date:  parseTime(seenAt),
			Price: price,
		})
	}
	return rows.Err()
}

func (s *Store) SetStatus(ctx context.Context, id, status string) error {
	if !validStatus(status) {
		return fmt.Errorf("%w: %s", ErrBadStatus, status)
	}
	if err := s.exists(ctx, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO statuses (ad_id, status, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(ad_id) DO UPDATE SET status = excluded.status,
		                                  updated_at = excluded.updated_at`,
		id, status, time.Now().Format(timestampLayout))
	if err != nil {
		return fmt.Errorf("set status: %w", err)
	}
	return nil
}

func (s *Store) ClearStatus(ctx context.Context, id string) error {
	if err := s.exists(ctx, id); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM statuses WHERE ad_id = ?`, id); err != nil {
		return fmt.Errorf("clear status: %w", err)
	}
	return nil
}

func (s *Store) exists(ctx context.Context, id string) error {
	var found int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM ads WHERE id = ?`, id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

type Counts struct {
	Listings int `json:"listings"`
	Active   int `json:"active"`
	Delisted int `json:"delisted"`
	Mapped   int `json:"mapped"`
	Marks    int `json:"marks"`
	Liked    int `json:"liked"`
}

func (s *Store) Counts(ctx context.Context) (Counts, error) {
	var c Counts
	err := s.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM ads),
		       (SELECT COUNT(*) FROM ads WHERE gone_at IS NULL),
		       (SELECT COUNT(*) FROM ads WHERE gone_at IS NOT NULL),
		       (SELECT COUNT(*) FROM ads WHERE lat IS NOT NULL),
		       (SELECT COUNT(*) FROM statuses),
		       (SELECT COUNT(*) FROM statuses WHERE status = 'liked')`).
		Scan(&c.Listings, &c.Active, &c.Delisted, &c.Mapped, &c.Marks, &c.Liked)
	if err != nil {
		return Counts{}, fmt.Errorf("counts: %w", err)
	}
	return c, nil
}

func (s *Store) Meta(ctx context.Context, key string) (string, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read meta %s: %w", key, err)
	}
	return value, nil
}

func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("write meta %s: %w", key, err)
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanListing(row rowScanner) (listing.Listing, error) {
	var (
		l                                    listing.Listing
		title, address, city, district       sql.NullString
		complexName, complexID, seller       sql.NullString
		ownerName, photo, flags, datePosted  sql.NullString
		link, status                         sql.NullString
		price, area, lat, lon                sql.NullFloat64
		rooms, floor, totalFloors, yearBuilt sql.NullInt64
		firstSeen, lastSeen, goneAt          sql.NullString
	)

	err := row.Scan(&l.ID, &title, &price, &area, &rooms, &floor, &totalFloors,
		&yearBuilt, &address, &city, &district, &complexName, &complexID, &seller,
		&ownerName, &photo, &lat, &lon, &flags, &datePosted, &link,
		&firstSeen, &lastSeen, &goneAt, &status)
	if err != nil {
		return listing.Listing{}, err
	}

	l.Title, l.Address, l.City, l.District = title.String, address.String, city.String, district.String
	l.Complex, l.ComplexID, l.Seller = complexName.String, complexID.String, seller.String
	l.OwnerName, l.Photo, l.Flags = ownerName.String, photo.String, flags.String
	l.DatePosted, l.Link, l.Status = datePosted.String, link.String, status.String
	l.Price, l.Area = price.Float64, area.Float64
	l.Rooms, l.Floor = int(rooms.Int64), int(floor.Int64)
	l.TotalFloors, l.YearBuilt = int(totalFloors.Int64), int(yearBuilt.Int64)
	if lat.Valid && lon.Valid {
		latitude, longitude := lat.Float64, lon.Float64
		l.Lat, l.Lon = &latitude, &longitude
	}
	l.FirstSeen, l.LastSeen = parseTime(firstSeen.String), parseTime(lastSeen.String)
	if goneAt.Valid && goneAt.String != "" {
		gone := parseTime(goneAt.String)
		l.GoneAt = &gone
	}
	return l, nil
}

// parseTime reads the local timestamps the scraper writes; anything unparsable
// becomes the zero time, which the API reports as "unknown" rather than failing.
func parseTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{timestampLayout, time.RFC3339, "2006-01-02T15:04:05.999999"} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func validStatus(status string) bool {
	for _, valid := range ValidStatuses {
		if status == valid {
			return true
		}
	}
	return false
}

func StatusList() string { return strings.Join(ValidStatuses, ", ") }

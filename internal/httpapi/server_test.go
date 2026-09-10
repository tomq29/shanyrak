package httpapi

import (
	"database/sql"
	"encoding/json"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/tomq29/shanyrak/internal/store"
	"github.com/tomq29/shanyrak/schema"
	"github.com/tomq29/shanyrak/web"
)

// seedAds writes listings the way the scraper does, since the store itself is
// read-only for everything except marks.
func seedAds(t *testing.T, path string, prices map[string]float64) {
	t.Helper()
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	defer raw.Close()

	if _, err := raw.Exec(schema.SQL); err != nil {
		t.Fatalf("schema: %v", err)
	}
	stamp := time.Now().Format("2006-01-02T15:04:05")
	for id, price := range prices {
		_, err := raw.Exec(`INSERT INTO ads
			(id, title, price, area, rooms, floor, total_floors, address, seller,
			 lat, lon, link, first_seen, last_seen)
			VALUES (?, '2-комн', ?, 50, 2, 3, 9, 'Абая 1', 'owner', 51.1, 71.4,
			        'https://krisha.kz/a/show/'||?, ?, ?)`, id, price, id, stamp, stamp)
		if err != nil {
			t.Fatalf("seed %s: %v", id, err)
		}
	}
}

func newServer(t *testing.T, seed map[string]float64) http.Handler {
	t.Helper()
	path := filepath.Join(t.TempDir(), "astana.db")
	if seed != nil {
		seedAds(t, path, seed)
	}

	db, err := store.Open(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	page, err := web.MapPage()
	if err != nil {
		t.Fatalf("parse page: %v", err)
	}
	server, err := New(
		[]Search{{Name: "astana", Store: db}, {Name: "almaty", Store: db}},
		page,
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	return server.Handler()
}

func twoListings() map[string]float64 {
	return map[string]float64{"1": 20_000_000, "2": 30_000_000}
}

func do(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func decode[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(recorder.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode %s: %v", recorder.Body.String(), err)
	}
	return value
}

func TestHealthz(t *testing.T) {
	response := do(t, newServer(t, nil), http.MethodGet, "/healthz", "")

	if response.Code != http.StatusOK || response.Body.String() != "ok" {
		t.Fatalf("healthz = %d %q", response.Code, response.Body.String())
	}
}

func TestRootRedirectsToTheFirstSearch(t *testing.T) {
	response := do(t, newServer(t, nil), http.MethodGet, "/", "")

	if response.Code != http.StatusFound || response.Header().Get("Location") != "/s/astana" {
		t.Fatalf("redirect = %d %q", response.Code, response.Header().Get("Location"))
	}
}

func TestMapPageCarriesTheBootData(t *testing.T) {
	response := do(t, newServer(t, nil), http.MethodGet, "/s/almaty", "")

	body := response.Body.String()
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if !strings.Contains(body, `"search":"almaty"`) {
		t.Fatalf("boot data missing from page: %s", body[:200])
	}
	if !strings.Contains(body, `"searches":["astana","almaty"]`) {
		t.Fatal("search list missing from page")
	}
}

func TestUnknownSearchIsNotFound(t *testing.T) {
	handler := newServer(t, nil)

	if code := do(t, handler, http.MethodGet, "/s/karaganda", "").Code; code != http.StatusNotFound {
		t.Fatalf("page status = %d, want 404", code)
	}
	if code := do(t, handler, http.MethodGet, "/api/searches/karaganda/listings", "").Code; code != http.StatusNotFound {
		t.Fatalf("api status = %d, want 404", code)
	}
}

func TestListingsReturnStatsAndItems(t *testing.T) {
	handler := newServer(t, twoListings())

	response := do(t, handler, http.MethodGet, "/api/searches/astana/listings", "")

	payload := decode[listingsResponse](t, response)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if payload.Stats.Count != 2 || payload.Stats.Median != 500_000 {
		t.Fatalf("stats = %+v", payload.Stats)
	}
	if len(payload.Items) != 2 || payload.Items[0].ID != "1" {
		t.Fatalf("items = %+v, want the cheaper one first", payload.Items)
	}
	if payload.Items[0].Lat == 0 {
		t.Fatal("coordinates missing from the response")
	}
}

func TestListingsOfAnEmptySearchIsAnEmptyList(t *testing.T) {
	handler := newServer(t, nil)

	response := do(t, handler, http.MethodGet, "/api/searches/astana/listings", "")

	if !strings.Contains(response.Body.String(), `"items":[]`) {
		t.Fatalf("body = %s, want an empty array", response.Body.String())
	}
}

func TestStatusRoundTrip(t *testing.T) {
	handler := newServer(t, twoListings())

	set := do(t, handler, http.MethodPut, "/api/searches/astana/listings/1/status", `{"status":"liked"}`)
	if set.Code != http.StatusNoContent {
		t.Fatalf("put = %d %s", set.Code, set.Body)
	}

	listings := decode[listingsResponse](t, do(t, handler, http.MethodGet, "/api/searches/astana/listings", ""))
	if listings.Items[0].Status != "liked" {
		t.Fatalf("status = %q, want liked", listings.Items[0].Status)
	}

	cleared := do(t, handler, http.MethodDelete, "/api/searches/astana/listings/1/status", "")
	if cleared.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", cleared.Code)
	}

	listings = decode[listingsResponse](t, do(t, handler, http.MethodGet, "/api/searches/astana/listings", ""))
	if listings.Items[0].Status != "" {
		t.Fatalf("status = %q, want empty", listings.Items[0].Status)
	}
}

func TestStatusRejectsBadInput(t *testing.T) {
	handler := newServer(t, twoListings())

	cases := []struct {
		name, path, body string
		want             int
	}{
		{"unknown status", "/api/searches/astana/listings/1/status", `{"status":"loved"}`, http.StatusBadRequest},
		{"broken json", "/api/searches/astana/listings/1/status", `{`, http.StatusBadRequest},
		{"unknown listing", "/api/searches/astana/listings/999/status", `{"status":"liked"}`, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := do(t, handler, http.MethodPut, tc.path, tc.body)
			if response.Code != tc.want {
				t.Fatalf("status = %d, want %d (%s)", response.Code, tc.want, response.Body)
			}
			if !strings.Contains(response.Body.String(), `"error"`) {
				t.Fatalf("body = %s, want an error message", response.Body)
			}
		})
	}
}

func TestListingByID(t *testing.T) {
	handler := newServer(t, twoListings())

	found := do(t, handler, http.MethodGet, "/api/searches/astana/listings/1", "")
	missing := do(t, handler, http.MethodGet, "/api/searches/astana/listings/999", "")

	if found.Code != http.StatusOK || !strings.Contains(found.Body.String(), `"ID":"1"`) {
		t.Fatalf("found = %d %s", found.Code, found.Body)
	}
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing = %d", missing.Code)
	}
}

func TestStatsCountTheDatabase(t *testing.T) {
	handler := newServer(t, twoListings())

	response := do(t, handler, http.MethodGet, "/api/searches/astana/stats", "")

	counts := decode[store.Counts](t, response)
	if counts.Listings != 2 || counts.Active != 2 || counts.Mapped != 2 {
		t.Fatalf("counts = %+v", counts)
	}
}

func TestSearchesList(t *testing.T) {
	handler := newServer(t, twoListings())

	response := do(t, handler, http.MethodGet, "/api/searches", "")

	items := decode[[]struct {
		Name     string `json:"name"`
		Listings int    `json:"listings"`
	}](t, response)
	if len(items) != 2 || items[0].Name != "astana" || items[0].Listings != 2 {
		t.Fatalf("searches = %+v", items)
	}
}

func TestMapPageIsValidTemplate(t *testing.T) {
	if _, err := template.New("x").Parse("{{.}}"); err != nil {
		t.Fatal(err)
	}
	if _, err := web.MapPage(); err != nil {
		t.Fatalf("map page: %v", err)
	}
}

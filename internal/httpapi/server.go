// Package httpapi serves the map page and the JSON API behind it.
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"time"

	"github.com/tomq29/shanyrak/internal/listing"
	"github.com/tomq29/shanyrak/internal/store"
)

// Search pairs a configured search with the database the scraper fills for it.
type Search struct {
	Name  string
	Store *store.Store
}

type Server struct {
	searches []Search
	byName   map[string]*store.Store
	page     *template.Template
	log      *slog.Logger
	now      func() time.Time
}

func New(searches []Search, page *template.Template, log *slog.Logger) (*Server, error) {
	if len(searches) == 0 {
		return nil, errors.New("no searches configured")
	}
	byName := make(map[string]*store.Store, len(searches))
	for _, search := range searches {
		byName[search.Name] = search.Store
	}
	return &Server{
		searches: searches,
		byName:   byName,
		page:     page,
		log:      log,
		now:      time.Now,
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /{$}", s.redirectToFirst)
	mux.HandleFunc("GET /s/{search}", s.mapPage)
	mux.HandleFunc("GET /api/searches", s.listSearches)
	mux.HandleFunc("GET /api/searches/{search}/listings", s.listings)
	mux.HandleFunc("GET /api/searches/{search}/listings/{id}", s.listingByID)
	mux.HandleFunc("PUT /api/searches/{search}/listings/{id}/status", s.setStatus)
	mux.HandleFunc("DELETE /api/searches/{search}/listings/{id}/status", s.clearStatus)
	mux.HandleFunc("GET /api/searches/{search}/stats", s.stats)
	return s.withLogging(mux)
}

func (s *Server) redirectToFirst(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/s/"+s.searches[0].Name, http.StatusFound)
}

type pageData struct {
	Search   string   `json:"search"`
	Searches []string `json:"searches"`
}

func (s *Server) mapPage(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("search")
	if _, ok := s.byName[name]; !ok {
		http.NotFound(w, r)
		return
	}
	data := pageData{Search: name}
	for _, search := range s.searches {
		data.Searches = append(data.Searches, search.Name)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.page.Execute(w, data); err != nil {
		s.log.Error("render map page", "error", err)
	}
}

func (s *Server) listSearches(w http.ResponseWriter, r *http.Request) {
	type item struct {
		Name string `json:"name"`
		store.Counts
	}
	items := make([]item, 0, len(s.searches))
	for _, search := range s.searches {
		counts, err := search.Store.Counts(r.Context())
		if err != nil {
			s.fail(w, r, err)
			return
		}
		items = append(items, item{Name: search.Name, Counts: counts})
	}
	s.writeJSON(w, r, http.StatusOK, items)
}

type listingsResponse struct {
	Search string         `json:"search"`
	Stats  listing.Stats  `json:"stats"`
	Items  []listing.View `json:"items"`
}

func (s *Server) listings(w http.ResponseWriter, r *http.Request) {
	db, ok := s.store(w, r)
	if !ok {
		return
	}
	listings, err := db.Listings(r.Context(), r.URL.Query().Has("include_gone"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	items, stats := listing.Analyze(listings, s.now())
	if items == nil {
		items = []listing.View{}
	}
	s.writeJSON(w, r, http.StatusOK, listingsResponse{
		Search: r.PathValue("search"), Stats: stats, Items: items,
	})
}

func (s *Server) listingByID(w http.ResponseWriter, r *http.Request) {
	db, ok := s.store(w, r)
	if !ok {
		return
	}
	found, err := db.Listing(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, found)
}

func (s *Server) setStatus(w http.ResponseWriter, r *http.Request) {
	db, ok := s.store(w, r)
	if !ok {
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "body must be {\"status\": \"...\"}")
		return
	}
	if err := db.SetStatus(r.Context(), r.PathValue("id"), body.Status); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) clearStatus(w http.ResponseWriter, r *http.Request) {
	db, ok := s.store(w, r)
	if !ok {
		return
	}
	if err := db.ClearStatus(r.Context(), r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	db, ok := s.store(w, r)
	if !ok {
		return
	}
	counts, err := db.Counts(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, r, http.StatusOK, counts)
}

func (s *Server) store(w http.ResponseWriter, r *http.Request) (*store.Store, bool) {
	db, ok := s.byName[r.PathValue("search")]
	if !ok {
		s.writeError(w, r, http.StatusNotFound, "unknown search")
		return nil, false
	}
	return db, true
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		s.writeError(w, r, http.StatusNotFound, "listing not found")
	case errors.Is(err, store.ErrBadStatus):
		s.writeError(w, r, http.StatusBadRequest, "status must be one of: "+store.StatusList())
	default:
		s.log.Error("request failed", "path", r.URL.Path, "error", err)
		s.writeError(w, r, http.StatusInternalServerError, "internal error")
	}
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, code int, message string) {
	s.writeJSON(w, r, code, map[string]string{"error": message})
}

func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, code int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		s.log.Error("write response", "path", r.URL.Path, "error", err)
	}
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		defer func() {
			if recovered := recover(); recovered != nil {
				s.log.Error("panic", "path", r.URL.Path, "value", fmt.Sprint(recovered))
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
			s.log.Info("request",
				"method", r.Method, "path", r.URL.Path,
				"status", recorder.status, "took", time.Since(started))
		}()

		next.ServeHTTP(recorder, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

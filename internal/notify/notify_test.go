package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tomq29/shanyrak/internal/listing"
)

var now = time.Date(2026, 9, 11, 12, 0, 0, 0, time.Local)

type fakeSource struct {
	listings []listing.Listing
	meta     map[string]string
	err      error
}

func (f *fakeSource) Listings(context.Context, bool) ([]listing.Listing, error) {
	return f.listings, f.err
}

func (f *fakeSource) Meta(_ context.Context, key string) (string, error) {
	return f.meta[key], nil
}

func (f *fakeSource) SetMeta(_ context.Context, key, value string) error {
	f.meta[key] = value
	return nil
}

type fakeSender struct {
	messages []string
	err      error
}

func (f *fakeSender) Send(_ context.Context, text string) error {
	if f.err != nil {
		return f.err
	}
	f.messages = append(f.messages, text)
	return nil
}

func ad(id string, price, area float64, history ...listing.PricePoint) listing.Listing {
	lat, lon := 51.1, 71.4
	return listing.Listing{
		ID: id, Price: price, Area: area, Address: "Абая " + id,
		Link: "https://krisha.kz/a/show/" + id,
		Lat:  &lat, Lon: &lon, FirstSeen: now.AddDate(0, 0, -1),
		History: history,
	}
}

func point(daysAgo int, price float64) listing.PricePoint {
	return listing.PricePoint{Date: now.AddDate(0, 0, -daysAgo), Price: price}
}

func watcher(source *fakeSource, sender Sender, deviation float64) *Watcher {
	w := New([]Search{{Name: "astana", Source: source}}, sender, deviation,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	w.now = func() time.Time { return now }
	return w
}

func TestFirstRunOnlyRecordsTheWatermark(t *testing.T) {
	source := &fakeSource{meta: map[string]string{}, listings: []listing.Listing{
		ad("1", 20_000_000, 50, point(1, 20_000_000)),
	}}
	sender := &fakeSender{}

	if err := watcher(source, sender, -10).Check(context.Background()); err != nil {
		t.Fatalf("check: %v", err)
	}

	if len(sender.messages) != 0 {
		t.Fatalf("messages = %v, want none on the first run", sender.messages)
	}
	if source.meta[metaKey] != now.Format(timeLayout) {
		t.Fatalf("watermark = %q", source.meta[metaKey])
	}
}

func TestReportsNewListingsBelowTheMedian(t *testing.T) {
	source := &fakeSource{
		meta: map[string]string{metaKey: now.AddDate(0, 0, -2).Format(timeLayout)},
		listings: []listing.Listing{
			ad("cheap", 20_000_000, 50, point(1, 20_000_000)),  // 400k, -20%
			ad("middle", 25_000_000, 50, point(9, 25_000_000)), // median, and old
			ad("dear", 30_000_000, 50, point(1, 30_000_000)),   // 600k, +20%
		},
	}
	sender := &fakeSender{}

	if err := watcher(source, sender, -10).Check(context.Background()); err != nil {
		t.Fatalf("check: %v", err)
	}

	if len(sender.messages) != 1 {
		t.Fatalf("messages = %v, want one", sender.messages)
	}
	message := sender.messages[0]
	if !strings.Contains(message, "a/show/cheap") {
		t.Fatalf("message misses the bargain: %s", message)
	}
	if strings.Contains(message, "a/show/dear") || strings.Contains(message, "a/show/middle") {
		t.Fatalf("message reports listings it should skip: %s", message)
	}
}

func TestReportsPriceDrops(t *testing.T) {
	source := &fakeSource{
		meta: map[string]string{metaKey: now.AddDate(0, 0, -2).Format(timeLayout)},
		listings: []listing.Listing{
			ad("dropped", 27_000_000, 50, point(9, 30_000_000), point(1, 27_000_000)),
			ad("raised", 33_000_000, 50, point(9, 30_000_000), point(1, 33_000_000)),
		},
	}
	sender := &fakeSender{}

	if err := watcher(source, sender, -10).Check(context.Background()); err != nil {
		t.Fatalf("check: %v", err)
	}

	message := sender.messages[0]
	if !strings.Contains(message, "Подешевели") || !strings.Contains(message, "a/show/dropped") {
		t.Fatalf("message misses the drop: %s", message)
	}
	if strings.Contains(message, "a/show/raised") {
		t.Fatalf("message reports a price rise: %s", message)
	}
}

func TestOldChangesAreNotRepeated(t *testing.T) {
	source := &fakeSource{
		meta: map[string]string{metaKey: now.Format(timeLayout)},
		listings: []listing.Listing{
			ad("cheap", 20_000_000, 50, point(3, 20_000_000)),
			ad("dropped", 27_000_000, 50, point(9, 30_000_000), point(3, 27_000_000)),
		},
	}
	sender := &fakeSender{}

	if err := watcher(source, sender, -10).Check(context.Background()); err != nil {
		t.Fatalf("check: %v", err)
	}

	if len(sender.messages) != 0 {
		t.Fatalf("messages = %v, want none", sender.messages)
	}
}

func TestWatermarkStaysWhenSendingFails(t *testing.T) {
	before := now.AddDate(0, 0, -2).Format(timeLayout)
	source := &fakeSource{
		meta: map[string]string{metaKey: before},
		listings: []listing.Listing{
			ad("cheap", 20_000_000, 50, point(1, 20_000_000)),
			ad("middle", 25_000_000, 50, point(9, 25_000_000)),
			ad("dear", 30_000_000, 50, point(9, 30_000_000)),
		},
	}
	sender := &fakeSender{err: errors.New("telegram down")}

	err := watcher(source, sender, -10).Check(context.Background())

	if err == nil {
		t.Fatal("want an error when the message cannot be sent")
	}
	if source.meta[metaKey] != before {
		t.Fatalf("watermark moved to %q, so the digest would be lost", source.meta[metaKey])
	}
}

func TestTelegramSendsToTheBotAPI(t *testing.T) {
	var got struct {
		ChatID string `json:"chat_id"`
		Text   string `json:"text"`
	}
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	telegram := NewTelegram("secret-token", "42")
	telegram.BaseURL = server.URL

	if err := telegram.Send(context.Background(), "привет"); err != nil {
		t.Fatalf("send: %v", err)
	}

	if path != "/botsecret-token/sendMessage" {
		t.Fatalf("path = %s", path)
	}
	if got.ChatID != "42" || got.Text != "привет" {
		t.Fatalf("payload = %+v", got)
	}
}

func TestTelegramReportsApiErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"ok":false,"description":"chat not found"}`, http.StatusBadRequest)
	}))
	defer server.Close()

	telegram := NewTelegram("token", "42")
	telegram.BaseURL = server.URL

	err := telegram.Send(context.Background(), "hi")

	if err == nil || !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("err = %v, want the api message", err)
	}
}

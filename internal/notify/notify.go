// Package notify reports new bargains and price cuts to Telegram.
package notify

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/tomq29/shanyrak/internal/listing"
)

const metaKey = "notified_at"
const timeLayout = "2006-01-02T15:04:05"

// Source is the part of the store the watcher needs.
type Source interface {
	Listings(ctx context.Context, includeGone bool) ([]listing.Listing, error)
	Meta(ctx context.Context, key string) (string, error)
	SetMeta(ctx context.Context, key, value string) error
}

type Sender interface {
	Send(ctx context.Context, text string) error
}

type Search struct {
	Name   string
	Source Source
}

type Watcher struct {
	searches  []Search
	sender    Sender
	deviation float64
	log       *slog.Logger
	now       func() time.Time
}

// New watches searches for listings priced at least `deviation` percent below
// the median of their search, e.g. -10 for ten percent cheaper.
func New(searches []Search, sender Sender, deviation float64, log *slog.Logger) *Watcher {
	return &Watcher{
		searches:  searches,
		sender:    sender,
		deviation: deviation,
		log:       log,
		now:       time.Now,
	}
}

func (w *Watcher) Run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		if err := w.Check(ctx); err != nil {
			w.log.Error("digest failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Watcher) Check(ctx context.Context) error {
	for _, search := range w.searches {
		if err := w.check(ctx, search); err != nil {
			return fmt.Errorf("%s: %w", search.Name, err)
		}
	}
	return nil
}

func (w *Watcher) check(ctx context.Context, search Search) error {
	raw, err := search.Source.Meta(ctx, metaKey)
	if err != nil {
		return err
	}
	now := w.now()

	// The first run only records the watermark: everything already collected
	// is old news and would arrive as one useless flood.
	if raw == "" {
		return search.Source.SetMeta(ctx, metaKey, now.Format(timeLayout))
	}
	since, err := time.ParseInLocation(timeLayout, raw, time.Local)
	if err != nil {
		return fmt.Errorf("bad %s value %q: %w", metaKey, raw, err)
	}

	listings, err := search.Source.Listings(ctx, false)
	if err != nil {
		return err
	}
	views, _ := listing.Analyze(listings, now)

	message := w.compose(search.Name, views, since)
	if message == "" {
		return search.Source.SetMeta(ctx, metaKey, now.Format(timeLayout))
	}
	if err := w.sender.Send(ctx, message); err != nil {
		return err
	}
	return search.Source.SetMeta(ctx, metaKey, now.Format(timeLayout))
}

func (w *Watcher) compose(search string, views []listing.View, since time.Time) string {
	var bargains, drops []string

	for _, view := range views {
		if isNew(view, since) && view.Deviation <= w.deviation {
			bargains = append(bargains, fmt.Sprintf("• %s · %s · %s\n  %s",
				millions(view.Price), squares(view.Area), percent(view.Deviation),
				view.Link))
		}
		if drop := latestDrop(view, since); drop != 0 {
			drops = append(drops, fmt.Sprintf("• %s → %s (%s) · %s\n  %s",
				millions(view.Price-drop), millions(view.Price), millions(drop),
				view.Address, view.Link))
		}
	}

	if len(bargains) == 0 && len(drops) == 0 {
		return ""
	}

	var out strings.Builder
	fmt.Fprintf(&out, "Shanyrak · %s\n", search)
	if len(bargains) > 0 {
		fmt.Fprintf(&out, "\nНиже медианы на %.0f%%+ (%d):\n%s\n",
			-w.deviation, len(bargains), strings.Join(bargains, "\n"))
	}
	if len(drops) > 0 {
		fmt.Fprintf(&out, "\nПодешевели (%d):\n%s\n", len(drops), strings.Join(drops, "\n"))
	}
	return strings.TrimSpace(out.String())
}

func isNew(view listing.View, since time.Time) bool {
	return len(view.History) > 0 && view.History[0].Date.After(since)
}

// latestDrop returns the size of the price cut recorded after `since`, or zero.
func latestDrop(view listing.View, since time.Time) float64 {
	for i := len(view.History) - 1; i > 0; i-- {
		point := view.History[i]
		if !point.Date.After(since) {
			return 0
		}
		if change := point.Price - view.History[i-1].Price; change < 0 {
			return change
		}
	}
	return 0
}

func millions(value float64) string {
	return strings.ReplaceAll(fmt.Sprintf("%.1f млн ₸", value/1e6), ".", ",")
}

func squares(value float64) string {
	return strings.ReplaceAll(fmt.Sprintf("%.0f м²", value), ".", ",")
}

func percent(value float64) string {
	return fmt.Sprintf("%+.0f%%", value)
}

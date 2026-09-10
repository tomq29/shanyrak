// Command shanyrak serves the map and the API over the databases the scraper fills.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tomq29/shanyrak/internal/config"
	"github.com/tomq29/shanyrak/internal/httpapi"
	"github.com/tomq29/shanyrak/internal/store"
	"github.com/tomq29/shanyrak/web"
)

func main() {
	configPath := flag.String("config", env("SHANYRAK_CONFIG", "shanyrak.toml"), "path to shanyrak.toml")
	addr := flag.String("addr", env("SHANYRAK_ADDR", ":8080"), "address to listen on")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if err := run(*configPath, *addr, log); err != nil {
		log.Error("shanyrak stopped", "error", err)
		os.Exit(1)
	}
}

func run(configPath, addr string, log *slog.Logger) error {
	conf, err := config.Load(configPath)
	if err != nil {
		return err
	}

	searches := make([]httpapi.Search, 0, len(conf.Searches))
	for _, name := range conf.Searches {
		db, err := store.Open(conf.DatabasePath(name))
		if err != nil {
			return err
		}
		defer db.Close()
		searches = append(searches, httpapi.Search{Name: name, Store: db})
		log.Info("search ready", "name", name, "database", conf.DatabasePath(name))
	}

	page, err := web.MapPage()
	if err != nil {
		return fmt.Errorf("parse map page: %w", err)
	}
	server, err := httpapi.New(searches, page, log)
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdown); err != nil {
			log.Error("shutdown", "error", err)
		}
	}()

	log.Info("listening", "addr", addr, "searches", conf.Searches)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

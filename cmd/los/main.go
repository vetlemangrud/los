// Command los serves the Los boat-watching site.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // distroless has no zoneinfo

	"los/internal/ais"
	"los/internal/config"
	"los/internal/geo"
	"los/internal/images"
	"los/internal/web"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	loc, err := time.LoadLocation("Europe/Oslo")
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	arc := geo.Arc{
		Center:   geo.Point{Lat: cfg.Lat, Lon: cfg.Lon},
		FromDeg:  cfg.ArcFrom,
		ToDeg:    cfg.ArcTo,
		RadiusNM: cfg.RadiusNM,
	}
	resolver := &images.Resolver{
		Finder:      images.NewCommons(),
		Concurrency: 4,
		Budget:      3 * time.Second,
		Logf:        log.Printf,
	}
	// Only assign Cache when the store opened: a nil *Store inside the
	// interface would be non-nil and panic on use.
	if store, err := images.OpenStore(cfg.DBPath); err != nil {
		log.Printf("image cache disabled: %v", err)
	} else {
		defer store.Close()
		resolver.Cache = store
		go store.RunPruner(ctx, 24*time.Hour, log.Printf)
	}

	srv := &web.Server{
		Arc:    arc,
		MaxAge: cfg.MaxAge,
		Boats:  ais.NewCache(ais.NewClient(cfg.ClientID, cfg.ClientSecret), 30*time.Second),
		Images: resolver,
		Now:    time.Now,
		Loc:    loc,
		Logf:   log.Printf,
	}
	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second, // > AIS fetch cap (12 s) + image budget (3 s)
		IdleTimeout:       60 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		log.Printf("los listening on %s (arc %g°→%g°, %g nm)", cfg.Addr, cfg.ArcFrom, cfg.ArcTo, cfg.RadiusNM)
		errc <- httpSrv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdownCtx)
}

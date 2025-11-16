// Package server sets up the Tubarr server.
package server

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/logger"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type serverStore struct {
	s  contracts.Store
	cs contracts.ChannelStore
	ds contracts.DownloadStore
	vs contracts.VideoStore
	db *sql.DB
}

var ss serverStore

const TubarrPort = "8827"

// NewRouter returns a http Handler.
func NewRouter(store contracts.Store, database *sql.DB) http.Handler {
	// Inject stores
	ss = serverStore{
		s:  store,
		cs: store.ChannelStore(),
		ds: store.DownloadStore(),
		vs: store.VideoStore(),
		db: database,
	}

	// Initialize router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// --- API Routes ---
	r.Route("/api/v1", func(r chi.Router) {
		// Channels API
		r.Route("/channels", func(r chi.Router) {
			r.Get("/all", handleListChannels)
			r.Post("/add", handleCreateChannel)
			r.Post("/add-from-file", handleAddChannelFromFile)
			r.Post("/add-from-directory", handleAddChannelsFromDir)
			r.Get("/{id}", handleGetChannel)
			r.Get("/{id}/downloads", handleGetDownloads)
			r.Get("/{id}/latest-downloads", handleLatestDownloads)
			r.Get("/{id}/all-videos", handleGetAllVideos)
			r.Put("/{id}/update", handleUpdateChannel)
			r.Delete("/{id}/delete", handleDeleteChannel)
			r.Delete("/{id}/delete-videos", handleDeleteChannelVideos)
			r.Delete("/{id}/cancel-download/{videoID}", handleCancelDownload)
			r.Post("/{id}/crawl", handleCrawlChannel)
			r.Post("/{id}/ignore-crawl", handleIgnoreCrawlChannel)
		})

		// Logs API
		r.Get("/logs", handleGetTubarrLogs)
		r.Get("/logs/metarr", handleGetMetarrLogs)
		r.Get("/logs/level", handleGetLogLevel)
		r.Post("/logs/level/{level}", handleSetLogLevel)
	})

	// --- Static Frontend ---
	// Serve compiled web UI for all unmatched routes (after API routes)
	staticHandler := StaticHandler()
	r.Handle("/*", staticHandler)

	return r
}

// StartServer starts the HTTP server on the specified port with graceful shutdown.
func StartServer(ctx context.Context, s contracts.Store, db *sql.DB) error {
	r := NewRouter(s, db)
	addr := ":" + TubarrPort

	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	// Start server in a goroutine
	serverErr := make(chan error, 1)
	go func() {
		logger.Pl.S("Tubarr web server running on http://localhost%s\n", addr)

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- fmt.Errorf("server failed: %w", err)
			return
		}

		serverErr <- nil
	}()

	// Start crawl watchdog in background (respects ctx cancellation)
	go ss.startCrawlWatchdog(ctx, nil)

	// Wait for interrupt signal
	select {
	case <-ctx.Done():
		logger.Pl.S("Shutting down server (context cancelled)...")

	case err := <-serverErr:
		// server crashed
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
	}

	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt graceful shutdown
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server forced to shutdown: %v", err)
	}

	logger.Pl.S("Server at http://localhost%s shut down successfully\n", addr)
	return nil
}

// StaticHandler handles serving of web pages.
func StaticHandler() http.Handler {
	exe, err := os.Executable()
	if err != nil {
		log.Printf("failed to get executable path, cannot locate web pages: %v", err)
		os.Exit(1)
	}
	baseDir := filepath.Dir(exe)
	webRoot := filepath.Join(baseDir, "web")

	if _, err := os.Stat(webRoot); err != nil {
		log.Printf("webpage root %q cannot be loaded: %v", webRoot, err)
		os.Exit(1)
	}

	fs := http.FileServer(http.Dir(webRoot))
	notFoundPage := filepath.Join(webRoot, "404.html")
	indexPage := filepath.Join(webRoot, "index.html")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanPath := filepath.Clean(r.URL.Path)
		fullPath := filepath.Join(webRoot, cleanPath)

		// Serve / → index.html
		if cleanPath == "/" {
			http.ServeFile(w, r, indexPage)
			return
		}

		// Serve existing file if present
		if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}

		// Fallback: if the path looks like a SPA route (no file extension)
		if !strings.Contains(filepath.Base(cleanPath), ".") {
			http.ServeFile(w, r, indexPage)
			return
		}

		// Otherwise, show 404 page if it exists
		if _, err := os.Stat(notFoundPage); err == nil {
			w.WriteHeader(http.StatusNotFound)
			http.ServeFile(w, r, notFoundPage)
			return
		}

		// Final fallback: plain 404 text
		http.NotFound(w, r)
	})
}

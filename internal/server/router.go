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

// serverStore holds the database, stores, and contexts for the server.
type serverStore struct {
	s      contracts.Store
	cs     contracts.ChannelStore
	ds     contracts.DownloadStore
	vs     contracts.VideoStore
	db     *sql.DB
	ctx    context.Context
	cancel context.CancelFunc
}

// tubarrPort.
const tubarrPort = "8827"

// NewRouter returns a http Handler.
func NewRouter(ss serverStore) http.Handler {
	// Initialize router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// --- Metarr Log ---
	// POST endpoint for Metarr instances to send logs.
	r.Post("/metarr-logs", ss.handlePostMetarrLogs)

	// --- API Routes ---
	r.Route("/api/v1", func(r chi.Router) {
		// Channels API.
		r.Route("/channels", func(r chi.Router) {
			r.Get("/all", ss.handleListChannels)
			r.Post("/add", ss.handleCreateChannel)
			r.Post("/add-from-file", ss.handleAddChannelFromFile)
			r.Post("/add-from-directory", ss.handleAddChannelsFromDir)
			r.Get("/{id}", ss.handleGetChannel)
			r.Get("/{id}/latest-downloads", ss.handleLatestDownloads)
			r.Get("/{id}/all-videos", ss.handleGetAllVideos)
			r.Put("/{id}/update", ss.handleUpdateChannel)
			r.Delete("/{id}/delete", ss.handleDeleteChannel)
			r.Delete("/{id}/delete-videos", ss.handleDeleteChannelVideos)
			r.Delete("/{id}/cancel-download/{videoID}", ss.handleCancelDownload)
			r.Post("/{id}/crawl", ss.handleCrawlChannel)
			r.Post("/{id}/ignore-crawl", ss.handleIgnoreCrawlChannel)
			r.Post("/{id}/download-urls", ss.handleDownloadURLs)
			r.Post("/{id}/notification-seen", ss.handleNewVideoNotificationSeen)
			r.Post("/{id}/toggle-pause", ss.handleTogglePauseChannel)
		})

		// Downloads API.
		r.Get("/downloads", ss.handleGetDownloads)

		// Logs API.
		r.Get("/logs", ss.handleGetTubarrLogs)
		r.Get("/logs/metarr", ss.handleGetMetarrLogs)
		r.Get("/logs/level", ss.handleGetLogLevel)
		r.Post("/logs/level/{level}", ss.handleSetLogLevel)

		// Blocked Domains API.
		r.Get("/blocked-domains", ss.handleGetBlockedDomains)
		r.Delete("/blocked-domains/{domain}", ss.handleUnblockDomain)
	})

	// --- Static Frontend ---
	// Serve compiled web UI for all unmatched routes (after API routes).
	staticHandler := StaticHandler()
	r.Handle("/*", staticHandler)

	return r
}

// StartServer starts the HTTP server on the specified port with graceful shutdown.
func StartServer(inputCtx context.Context, inputCtxCancel context.CancelFunc, store contracts.Store, database *sql.DB) error {
	ss := serverStore{
		s:      store,
		cs:     store.ChannelStore(),
		ds:     store.DownloadStore(),
		vs:     store.VideoStore(),
		db:     database,
		ctx:    inputCtx,
		cancel: inputCtxCancel,
	}

	r := NewRouter(ss)
	addr := ":" + tubarrPort

	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	// Start server in a goroutine
	serverErr := make(chan error, 1)
	go func() {
		logger.Pl.S("Tubarr web server listening on http://localhost%s\n", srv.Addr)

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- fmt.Errorf("server failed: %w", err)
			return
		}

		serverErr <- nil
	}()

	// Start crawl watchdog in background (respects ctx cancellation).
	go ss.startCrawlWatchdog(inputCtx, nil)

	// Wait for interrupt signal.
	select {
	case <-inputCtx.Done():
		logger.Pl.S("Shutting down server (context cancelled)...")

	case err := <-serverErr:
		// server crashed
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
	}

	// Create shutdown context with timeout.
	shutdownCtx, cancel := context.WithTimeout(inputCtx, 10*time.Second)
	defer cancel()

	// Attempt graceful shutdown.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server forced to shutdown: %w", err)
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

// Package server sets up the Tubarr server.
package server

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"tubarr/internal/contracts"
	"tubarr/internal/utils/logging"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

var (
	s  contracts.Store
	cs contracts.ChannelStore
	ds contracts.DownloadStore
	vs contracts.VideoStore
)

const TubarrPort = "8827"

// NewRouter returns a http Handler.
func NewRouter(store contracts.Store) http.Handler {
	// Inject stores
	s = store
	cs = s.ChannelStore()
	ds = s.DownloadStore()
	vs = s.VideoStore()

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
			r.Get("/{id}", handleGetChannel)
			r.Get("/{id}/downloads", handleLatestDownloads)
			r.Put("/{id}/update", handleUpdateChannel)
			r.Delete("/{id}/delete", handleDeleteChannel)

			// // Channel URLs
			// r.Route("/{id}/urls", func(r chi.Router) {
			// 	r.Get("/", handleListChannelURLs)
			// 	r.Post("/", handleAddChannelURL)
			// 	r.Get("/{urlID}", handleGetChannelURL)
			// 	r.Put("/{urlID}", handleUpdateChannelURL)
			// 	r.Delete("/{urlID}", handleDeleteChannelURL)
			// })
		})

		// Downloads API
		// r.Route("/downloads", func(r chi.Router) {
		// 	r.Get("/latest", handleLatestDownloads)
		// 	r.Delete("/{videoURLID}", handleDeleteVideoURL)
		// })
	})

	// --- Static Frontend ---
	// Serve compiled web UI for all unmatched routes (after API routes)
	staticHandler := StaticHandler()
	r.Handle("/*", staticHandler)

	return r
}

// StartServer starts the HTTP server on the specified port with graceful shutdown.
func StartServer(s contracts.Store) {
	r := NewRouter(s)
	addr := ":" + TubarrPort

	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	// Channel to listen for interrupt signals
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// Start server in a goroutine
	go func() {
		logging.S("Tubarr web server running on http://localhost%s\n", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server failed: %v", err)
		}
	}()

	// Wait for interrupt signal
	<-stop
	logging.S("\nShutting down server...\n")

	// Create shutdown context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Attempt graceful shutdown
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("server forced to shutdown: %v", err)
	}

	logging.S("Server at http://localhost%s shut down successfully\n", addr)
}

// StaticHandler handles serving of web pages.
func StaticHandler() http.Handler {
	exe, err := os.Executable()
	if err != nil {
		log.Fatalf("failed to get executable path, cannot locate web pages: %v", err)
	}
	baseDir := filepath.Dir(exe)
	webRoot := filepath.Join(baseDir, "web", "dist")

	if _, err := os.Stat(webRoot); err != nil {
		log.Fatalf("webpage root %q cannot be loaded: %v", webRoot, err)
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

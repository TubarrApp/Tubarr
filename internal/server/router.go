// Package server sets up the Tubarr server.
package server

import (
	"log"
	"net/http"
	"tubarr/internal/contracts"
	"tubarr/internal/utils/logging"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

var (
	cs contracts.ChannelStore
	ds contracts.DownloadStore
	vs contracts.VideoStore
)

const TubarrPort = "8827"

// NewRouter returns a http Handler.
func NewRouter(s contracts.Store) http.Handler {
	// Inject stores
	cs = s.ChannelStore()
	ds = s.DownloadStore()
	vs = s.VideoStore()

	// Initialize router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// --- Static Frontend ---
	// Serve compiled web UI for non-API routes.
	r.Handle("/*", StaticHandler())

	// --- API Routes ---
	r.Route("/api/v1", func(r chi.Router) {
		// Channels API
		r.Route("/channels", func(r chi.Router) {
			r.Get("/", handleListChannels)
			r.Post("/", handleCreateChannel)
			r.Get("/{id}", handleGetChannel)
			r.Get("/{id}/latest", handleLatestDownloads)
			r.Put("/{id}", handleUpdateChannel)
			r.Delete("/{id}", handleDeleteChannel)

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

	return r
}

// StartServer starts the HTTP server on the specified port.
func StartServer(s contracts.Store) {
	r := NewRouter(s)
	addr := ":" + TubarrPort
	logging.S("Tubarr web server running on http://localhost%s\n", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

func StaticHandler() http.Handler {
	fs := http.FileServer(http.Dir("./web/dist")) // Svelte build location
	return http.StripPrefix("/", fs)
}

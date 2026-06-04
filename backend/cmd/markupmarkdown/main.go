package main

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

	"github.com/gorilla/mux"
	"github.com/rs/cors"

	"markupmarkdown/internal/api"
	"markupmarkdown/internal/config"
	"markupmarkdown/internal/mcpserver"
	"markupmarkdown/internal/store"
)

// collectAdditionalKeys scans the environment for prior key versions:
// MARKUPMARKDOWN_ENCRYPTION_KEY_V2, _V3, etc.
func collectAdditionalKeys() map[string]string {
	out := map[string]string{}
	const prefix = "MARKUPMARKDOWN_ENCRYPTION_KEY_V"
	for _, kv := range os.Environ() {
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		k, v := kv[:eq], kv[eq+1:]
		if !strings.HasPrefix(k, prefix) || v == "" {
			continue
		}
		version := strings.ToLower(strings.TrimPrefix(k, "MARKUPMARKDOWN_ENCRYPTION_KEY_"))
		out[version] = v
	}
	return out
}

func main() {
	config.LoadEnvFile(".env")

	env := os.Getenv("MARKUPMARKDOWN_ENV")
	if env == "" {
		env = "dev"
	}

	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = filepath.Join("config", env+".yaml")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("config load: %v", err)
	}
	// Collect any prior key versions for at-rest secret rotation.
	cfg.Encryption.AdditionalKeys = collectAdditionalKeys()
	log.Printf("Starting markupmarkdown [%s] (port %s)", env, cfg.Server.Port)

	st, err := store.New(cfg.Database.URI, cfg.Database.Name)
	if err != nil {
		log.Fatalf("store init: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = st.Close(ctx)
	}()
	log.Printf("Connected to MongoDB (%s)", cfg.Database.Name)

	r := mux.NewRouter()
	a, err := api.New(cfg, st)
	if err != nil {
		log.Fatalf("api init: %v", err)
	}
	a.Register(r)
	a.StartPurgeSweep()

	// MCP server for agents: streamable HTTP transport, Bearer-token auth.
	mcpHandler := mcpserver.New(a, st, cfg.Frontend.URL)
	r.PathPrefix("/mcp").Handler(mcpHandler)

	// In prod, serve the built frontend from the same origin (catch-all).
	if cfg.Frontend.StaticDir != "" {
		log.Printf("Serving frontend from %s", cfg.Frontend.StaticDir)
		r.PathPrefix("/").Handler(api.SPAHandler{
			StaticDir: cfg.Frontend.StaticDir,
			Store:     st,
			SiteURL:   cfg.Frontend.URL,
		})
	}

	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   []string{cfg.Frontend.URL},
		AllowedMethods:   []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "Authorization"},
		AllowCredentials: true,
	}).Handler(r)

	srv := &http.Server{
		Addr:              cfg.Server.Host + ":" + cfg.Server.Port,
		Handler:           corsHandler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("HTTP listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Println("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/its-the-vibe/QueryLab/internal/config"
	"github.com/its-the-vibe/QueryLab/internal/poppit"
	"github.com/its-the-vibe/QueryLab/internal/web"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	executor := poppit.NewExecutor(*cfg)
	defer executor.Close()

	server, err := web.NewServer(cfg, executor)
	if err != nil {
		log.Fatalf("failed to initialize web server: %v", err)
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("QueryLab listening on %s", server.Addr)
		err := server.ListenAndServe()
		if !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serverErrors:
		log.Fatalf("server failed: %v", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}

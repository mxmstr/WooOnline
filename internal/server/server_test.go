package server

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"stranglehold-go-server/internal/config"
)

func TestRunStopsWhenContextIsCanceled(t *testing.T) {
	cfg := config.Config{
		BindHost: "127.0.0.1", PublicHost: "127.0.0.1",
		AuthPort: 0, SecurePort: 0, NATPort: 0,
		DatabasePath: filepath.Join(t.TempDir(), "server.db"),
	}
	app, err := New(cfg, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		app.Close()
		t.Fatal("server did not stop after context cancellation")
	}
}

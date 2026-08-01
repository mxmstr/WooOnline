package server

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"stranglehold-go-server/internal/config"
	"stranglehold-go-server/internal/protocols"
	"stranglehold-go-server/internal/prudp"
	"stranglehold-go-server/internal/rmc"
	"stranglehold-go-server/internal/state"
	"stranglehold-go-server/internal/store"
)

type App struct {
	config      config.Config
	logger      *slog.Logger
	database    *store.DB
	gatherings  *state.GatheringRegistry
	servers     []*prudp.Server
	dispatchers []*rmc.Dispatcher
	closeOnce   sync.Once
}

func New(cfg config.Config, logger *slog.Logger) (*App, error) {
	database, err := store.Open(cfg.DatabasePath)
	if err != nil {
		return nil, err
	}
	if cfg.AccountsFile != "" {
		imported, skipped, err := store.ImportAccountsFile(context.Background(), database, cfg.AccountsFile)
		if err != nil {
			_ = database.Close()
			return nil, err
		}
		logger.Info("preloaded accounts", "file", cfg.AccountsFile, "imported", imported, "skipped", skipped)
	}
	identity := state.NewIdentityRegistry()
	gatherings := state.NewGatheringRegistry(cfg.KeepOldMatches, logger)
	services := protocols.NewServices(cfg, database, identity, gatherings, logger)
	app := &App{config: cfg, logger: logger, database: database, gatherings: gatherings}

	for _, port := range []int{cfg.AuthPort, cfg.SecurePort, cfg.NATPort} {
		transport := prudp.NewServer(port, cfg.AccessKey, cfg.RC4Key, logger)
		dispatcher := rmc.NewDispatcher(transport, logger)
		services.RegisterAll(dispatcher)
		transport.SetPayloadHandler(dispatcher.OnPayload)
		if port == cfg.SecurePort || port == cfg.NATPort {
			transport.SetConnectACKHandler(services.HandleConnectACK)
		}
		transport.AddCloseHandler(func(remote string) {
			destroyed := gatherings.DestroyByOwnerRemote(remote)
			removed := gatherings.RemoveParticipantByRemote(remote)
			if destroyed > 0 || removed > 0 {
				logger.Info("cleaned connection matchmaking state", "remote", remote, "destroyed", destroyed, "removed", removed)
			}
		})
		app.servers = append(app.servers, transport)
		app.dispatchers = append(app.dispatchers, dispatcher)
	}
	return app, nil
}

func (a *App) Run(ctx context.Context) error {
	for _, transport := range a.servers {
		if err := transport.Listen(a.config.BindHost); err != nil {
			a.Close()
			return err
		}
	}
	a.logger.Info("Stranglehold server ready",
		"auth_port", a.config.AuthPort,
		"secure_port", a.config.SecurePort,
		"nat_port", a.config.NATPort,
		"authentication", "stranglehold-password",
		"ps3_kerberos", "disabled")

	errorChannel := make(chan error, len(a.servers))
	for _, transport := range a.servers {
		go func(server *prudp.Server) {
			errorChannel <- server.Serve(ctx)
		}(transport)
	}
	go a.reapLoop(ctx)

	select {
	case <-ctx.Done():
		a.Close()
		for range a.servers {
			<-errorChannel
		}
		return nil
	case err := <-errorChannel:
		a.Close()
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		return nil
	}
}

func (a *App) reapLoop(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, transport := range a.servers {
				transport.ReapIdle(90 * time.Second)
			}
		}
	}
}

func (a *App) Close() {
	a.closeOnce.Do(func() {
		for _, transport := range a.servers {
			_ = transport.Close()
		}
		if a.database != nil {
			_ = a.database.Close()
		}
	})
}

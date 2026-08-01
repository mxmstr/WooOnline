package protocols

import (
	"log/slog"
	"sync"

	"stranglehold-go-server/internal/config"
	"stranglehold-go-server/internal/rmc"
	"stranglehold-go-server/internal/state"
	"stranglehold-go-server/internal/store"
)

type Services struct {
	Config      config.Config
	Store       *store.DB
	Identity    *state.IdentityRegistry
	Gatherings  *state.GatheringRegistry
	Logger      *slog.Logger
	authMu      sync.RWMutex
	credentials map[string]credential
	sessionKeys map[uint32][]byte
	statsMu     sync.Mutex
	statsTxn    map[uint32]uint32
}

func NewServices(cfg config.Config, database *store.DB, identity *state.IdentityRegistry, gatherings *state.GatheringRegistry, logger *slog.Logger) *Services {
	services := &Services{
		Config: cfg, Store: database, Identity: identity, Gatherings: gatherings,
		Logger: logger, credentials: make(map[string]credential),
		sessionKeys: make(map[uint32][]byte), statsTxn: make(map[uint32]uint32),
	}
	services.credentials["guest"] = credential{
		PID: 100, Password: []byte("h7fyctiuucf"),
	}
	return services
}

func (s *Services) RegisterAll(dispatcher *rmc.Dispatcher) {
	s.registerAuthentication(dispatcher)
	s.registerSecure(dispatcher)
	s.registerAccounts(dispatcher)
	s.registerMatchmaking(dispatcher)
	s.registerNAT(dispatcher)
	s.registerGame(dispatcher)
}

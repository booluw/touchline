package httpapi

import (
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"

	internalauth "github.com/touchline/backend/internal/auth"
	internalbootstrap "github.com/touchline/backend/internal/bootstrap"
	internalclub "github.com/touchline/backend/internal/club"
	internalcompetition "github.com/touchline/backend/internal/competition"
	internalfinance "github.com/touchline/backend/internal/finance"
	internalmanager "github.com/touchline/backend/internal/manager"
	internalmatch "github.com/touchline/backend/internal/match"
	internaltactics "github.com/touchline/backend/internal/tactics"
	internaltraining "github.com/touchline/backend/internal/training"
	internaltransfer "github.com/touchline/backend/internal/transfer"
	internalworld "github.com/touchline/backend/internal/world"
	pkgjwt "github.com/touchline/backend/pkg/auth"
	"github.com/touchline/backend/pkg/realtime"
)

// server wires the REST + WebSocket surface to the game services. Handlers
// resolve world/manager scoping from the session identity and the
// manager.managers row at request time (OPD-15).
type server struct {
	svc           *internalauth.Service
	worldSvc      *internalworld.Service
	mgrSvc        *internalmanager.Service
	clubSvc       *internalclub.Service
	bootSvc       *internalbootstrap.Service
	compSvc       *internalcompetition.Service
	matchSvc      *internalmatch.Service
	tacticsSvc    *internaltactics.Service
	trainingSvc   *internaltraining.Service
	financeSvc    *internalfinance.Service
	transfersSvc  *internaltransfer.Service
	jwtCfg        pkgjwt.JWTConfig
	pool          *pgxpool.Pool
	cookiesSecure bool
	appOrigin     string
	hub           *realtime.Hub
}

// Options assembles a server. Every service is already constructed and bound to
// its event bus by the caller (see internal/app); the server only wires them to
// HTTP routes.
type Options struct {
	Auth          *internalauth.Service
	World         *internalworld.Service
	Manager       *internalmanager.Service
	Club          *internalclub.Service
	Bootstrap     *internalbootstrap.Service
	Competition   *internalcompetition.Service
	Match         *internalmatch.Service
	Tactics       *internaltactics.Service
	Training      *internaltraining.Service
	Finance       *internalfinance.Service
	Transfers     *internaltransfer.Service
	JWT           pkgjwt.JWTConfig
	Pool          *pgxpool.Pool
	CookiesSecure bool
	AppOrigin     string
	Hub           *realtime.Hub
}

// New builds a server from the supplied services.
func New(opts Options) *Server {
	return &Server{server: server{
		svc:           opts.Auth,
		worldSvc:      opts.World,
		mgrSvc:        opts.Manager,
		clubSvc:       opts.Club,
		bootSvc:       opts.Bootstrap,
		compSvc:       opts.Competition,
		matchSvc:      opts.Match,
		tacticsSvc:    opts.Tactics,
		trainingSvc:   opts.Training,
		financeSvc:    opts.Finance,
		transfersSvc:  opts.Transfers,
		jwtCfg:        opts.JWT,
		pool:          opts.Pool,
		cookiesSecure: opts.CookiesSecure,
		appOrigin:     opts.AppOrigin,
		hub:           opts.Hub,
	}}
}

// Server is the exported handle around the embedded HTTP wiring.
type Server struct {
	server
}

// Handler returns the fully wired gin engine.
func (s *Server) Handler() *gin.Engine {
	return s.server.router()
}

// Hub exposes the realtime hub for tests and producers that publish to
// connected sockets (e.g. the worker's world-tick bridge).
func (s *Server) Hub() *realtime.Hub {
	return s.server.hub
}

// OriginHostPattern converts APP_ORIGIN (e.g. "http://localhost:3000") into a
// WebSocket origin pattern (e.g. "localhost:3000"). coder/websocket matches
// these against the browser's Origin header to stop cross-site socket
// hijacking; with no configured origin every WS path falls back to same-host
// checks (fine for bare `go run`).
func OriginHostPattern(appOrigin string) []string {
	if appOrigin == "" {
		return nil
	}
	u, err := url.Parse(appOrigin)
	if err != nil {
		return []string{appOrigin}
	}
	return []string{u.Host}
}

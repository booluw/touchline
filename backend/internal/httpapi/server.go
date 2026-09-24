package httpapi

import (
	"context"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	internalacademy "github.com/touchline/backend/internal/academy"
	internaladmin "github.com/touchline/backend/internal/admin"
	internalauth "github.com/touchline/backend/internal/auth"
	internalboard "github.com/touchline/backend/internal/board"
	internalbootstrap "github.com/touchline/backend/internal/bootstrap"
	internalclub "github.com/touchline/backend/internal/club"
	internalcompetition "github.com/touchline/backend/internal/competition"
	internaldashboard "github.com/touchline/backend/internal/dashboard"
	internalfaction "github.com/touchline/backend/internal/faction"
	internalfinance "github.com/touchline/backend/internal/finance"
	internalmanager "github.com/touchline/backend/internal/manager"
	internalmatch "github.com/touchline/backend/internal/match"
	internalplayer "github.com/touchline/backend/internal/player"
	"github.com/touchline/backend/internal/policybot"
	internalscout "github.com/touchline/backend/internal/scout"
	internalsocial "github.com/touchline/backend/internal/social"
	internaltactics "github.com/touchline/backend/internal/tactics"
	internaltraining "github.com/touchline/backend/internal/training"
	internaltransfer "github.com/touchline/backend/internal/transfer"
	internalworld "github.com/touchline/backend/internal/world"
	pkgjwt "github.com/touchline/backend/pkg/auth"
	"github.com/touchline/backend/pkg/eventbus"
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
	boardSvc      *internalboard.Service
	playerSvc     *internalplayer.Service
	socialSvc     *internalsocial.Service
	policySvc     *policybot.Service
	dashSvc       *internaldashboard.Service
	academySvc    *internalacademy.Service
	factionSvc    *internalfaction.Service
	adminSvc      *internaladmin.Service
	scoutSvc      *internalscout.Service
	seedJobs      func(ctx context.Context, worldID uuid.UUID) (int64, error)
	jwtCfg        pkgjwt.JWTConfig
	pool          *pgxpool.Pool
	cookiesSecure bool
	appOrigin     string
	hub           *realtime.Hub
	bus           eventbus.Publisher
}

// Options assembles a server. Every service is already constructed and bound to
// its event bus by the caller (see internal/app); the server only wires them to
// HTTP routes.
type Options struct {
	Auth        *internalauth.Service
	World       *internalworld.Service
	Manager     *internalmanager.Service
	Club        *internalclub.Service
	Bootstrap   *internalbootstrap.Service
	Competition *internalcompetition.Service
	Match       *internalmatch.Service
	Tactics     *internaltactics.Service
	Training    *internaltraining.Service
	Finance     *internalfinance.Service
	Transfers   *internaltransfer.Service
	Board       *internalboard.Service
	Player      *internalplayer.Service
	Social      *internalsocial.Service
	Policy      *policybot.Service
	Dashboard   *internaldashboard.Service
	Academy     *internalacademy.Service
	Faction     *internalfaction.Service
	Admin       *internaladmin.Service
	Scout       *internalscout.Service
	// SeedJobs enqueues an async world-seed job and returns the river job id.
	// The caller supplies it (wired over eventbus.InsertJob) so the HTTP layer
	// never depends on the river client directly.
	SeedJobs      func(ctx context.Context, worldID uuid.UUID) (int64, error)
	JWT           pkgjwt.JWTConfig
	Pool          *pgxpool.Pool
	CookiesSecure bool
	AppOrigin     string
	Hub           *realtime.Hub
	Bus           eventbus.Publisher
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
		boardSvc:      opts.Board,
		playerSvc:     opts.Player,
		socialSvc:     opts.Social,
		policySvc:     opts.Policy,
		dashSvc:       opts.Dashboard,
		academySvc:    opts.Academy,
		factionSvc:    opts.Faction,
		adminSvc:      opts.Admin,
		scoutSvc:      opts.Scout,
		jwtCfg:        opts.JWT,
		pool:          opts.Pool,
		cookiesSecure: opts.CookiesSecure,
		appOrigin:     opts.AppOrigin,
		hub:           opts.Hub,
		bus:           opts.Bus,
		seedJobs:      opts.SeedJobs,
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

// Command touchline runs the Touchline backend from a single binary. The
// default "serve" mode runs the API (HTTP + WS), the scheduler (world clock),
// and the worker (event consumer + live match engine) in one process — the game
// is only playable when all three run together, so this is the primary dev and
// deployment entry point. The api/scheduler/worker subcommands run a single
// subsystem for prod isolation.
package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/touchline/backend/internal/app"
)

func main() {
	role := "serve"
	if len(os.Args) > 1 {
		role = os.Args[1]
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := app.FromEnv()
	if err != nil {
		log.Fatal(err)
	}

	a, err := app.Build(ctx, cfg)
	if err != nil {
		log.Fatal(err)
	}
	defer a.Close()

	switch role {
	case "serve":
		err = a.RunAll(ctx)
	case "api":
		err = a.RunAPI(ctx)
	case "scheduler":
		err = a.RunScheduler(ctx)
	case "worker":
		err = a.RunWorker(ctx)
	default:
		log.Fatalf("unknown role %q — want serve|api|scheduler|worker", role)
	}

	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("%s: %v", role, err)
	}
	log.Printf("touchline %s stopped", role)
}

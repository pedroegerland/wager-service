package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/fx"

	"github.com/pedroegerland/wager-service/internal/config"
	"github.com/pedroegerland/wager-service/internal/fxapp"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	app := fx.New(fxapp.Options(cfg))

	startCtx, cancel := context.WithTimeout(context.Background(), app.StartTimeout())
	defer cancel()
	if err := app.Start(startCtx); err != nil {
		fmt.Fprintln(os.Stderr, "start:", err)
		os.Exit(1)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	stopCtx, cancel := context.WithTimeout(context.Background(), app.StopTimeout())
	defer cancel()
	if err := app.Stop(stopCtx); err != nil {
		fmt.Fprintln(os.Stderr, "stop:", err)
		os.Exit(1)
	}
}

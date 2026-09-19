package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/sezznaw/devkit/cmd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := cmd.Execute(ctx); err != nil {
		os.Exit(1)
	}
}

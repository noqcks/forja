package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/noqcks/forja/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cli.Execute(ctx); err != nil {
		os.Exit(1)
	}
}

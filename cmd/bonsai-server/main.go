// bonsai-server is kept as a deployment-compatible alias for the relay.
// New deployments should use bonsai-relay.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Tiago-0liveira/bonsai/internal/server/relay"
)

type config struct {
	Address        string `json:"address"`
	Database       string `json:"database"`
	ExternalURL    string `json:"external_url"`
	FrontendOrigin string `json:"frontend_origin"`
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: bonsai-server <config.json>")
	}
	body, err := os.ReadFile(os.Args[1])
	if err != nil {
		return err
	}
	var file config
	if err := json.Unmarshal(body, &file); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return relay.Run(ctx, relay.Config{
		Address:            file.Address,
		Database:           file.Database,
		ExternalURL:        file.ExternalURL,
		FrontendOrigin:     file.FrontendOrigin,
		GitHubClientID:     os.Getenv("GITHUB_APP_CLIENT_ID"),
		GitHubClientSecret: os.Getenv("GITHUB_APP_CLIENT_SECRET"),
		WebhookSecret:      os.Getenv("GITHUB_WEBHOOK_SECRET"),
	})
}

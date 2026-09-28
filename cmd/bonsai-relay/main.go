// bonsai-relay is the deliberately small internet-facing GitHub event relay.
// It has no local repository, daemon, process, or command execution surface.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Tiago-0liveira/bonsai/internal/server/relay"
)

type fileConfig struct {
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

func envOverride(current string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return current
}

func loadConfig() (fileConfig, error) {
	if len(os.Args) > 2 {
		return fileConfig{}, fmt.Errorf("usage: bonsai-relay [config.json]")
	}
	var file fileConfig
	if len(os.Args) == 2 {
		body, err := os.ReadFile(os.Args[1])
		if err != nil {
			return fileConfig{}, err
		}
		if err := json.Unmarshal(body, &file); err != nil {
			return fileConfig{}, err
		}
	}

	file.Address = envOverride(file.Address, "BONSAI_RELAY_ADDRESS")
	if file.Address == "" {
		if port := strings.TrimSpace(os.Getenv("PORT")); port != "" {
			file.Address = "0.0.0.0:" + port
		}
	}
	file.Database = envOverride(file.Database, "BONSAI_RELAY_DATABASE")
	file.ExternalURL = envOverride(file.ExternalURL, "BONSAI_RELAY_EXTERNAL_URL", "BONSAI_RELAY_ORIGIN")
	file.FrontendOrigin = envOverride(file.FrontendOrigin, "BONSAI_RELAY_FRONTEND_ORIGIN", "BONSAI_FRONTEND_ORIGIN")
	return file, nil
}

func run() error {
	file, err := loadConfig()
	if err != nil {
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

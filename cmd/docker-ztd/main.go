package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/app"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/cli"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/logging"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "docker-cli-plugin-metadata" {
		fmt.Print(cli.PluginMetadata())
		return
	}

	cfg, err := cli.Parse(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		fmt.Print(cli.Usage())
		os.Exit(1)
	}

	if cfg.ShowHelp {
		fmt.Print(cli.Usage())
		return
	}

	if cfg.Service == "" {
		fmt.Fprintln(os.Stderr, "SERVICE is missing")
		fmt.Print(cli.Usage())
		os.Exit(1)
	}

	log := logging.NewLogger()
	runner := app.NewRunner(log)
	if err := runner.Run(context.Background(), cfg); err != nil {
		log.Error(err.Error())
		os.Exit(1)
	}
}

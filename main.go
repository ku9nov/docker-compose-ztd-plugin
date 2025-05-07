package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ku9nov/docker-compose-ztd-plugin/docker"
	"github.com/ku9nov/docker-compose-ztd-plugin/traefik"
	"github.com/sirupsen/logrus"
)

// PluginMetadata represents the plugin metadata
type PluginMetadata struct {
	SchemaVersion    string   `json:"SchemaVersion"`
	Vendor           string   `json:"Vendor"`
	Version          string   `json:"Version"`
	ShortDescription string   `json:"ShortDescription"`
	URL              string   `json:"URL"`
	Experimental     bool     `json:"Experimental"`
	Commands         []string `json:"Commands"`
}

// Config holds the configuration parameters
type Config struct {
	HealthcheckTimeout    time.Duration
	NoHealthcheckTimeout  time.Duration
	WaitAfterHealthyDelay time.Duration
	TraefikConfFile       string
	ProxyType             string
	ComposeFiles          []string
	EnvFiles              []string
	Service               string
	Detach                bool
}

func main() {
	// Check if the first argument is docker-cli-plugin-metadata
	if len(os.Args) > 1 && os.Args[1] == "docker-cli-plugin-metadata" {
		printPluginMetadata()
		return
	}

	// Initialize logger
	log := logrus.New()
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	// Parse command line arguments
	config := parseArgs(log)

	// Create Docker client
	dockerClient, err := docker.NewDockerClient(log)
	if err != nil {
		log.Fatalf("Failed to create Docker client: %v", err)
	}

	// Set compose files
	dockerClient.SetComposeFiles(config.ComposeFiles)

	// Set Traefik config path
	dockerClient.SetTraefikConf(config.TraefikConfFile)

	// Create context with timeout
	ctx := context.Background()

	// Handle different service commands
	if config.Service == "up" || config.Service == "up -d" {
		detach := config.Service == "up -d"
		if err := dockerClient.DeployService(ctx, "up", detach, config.ComposeFiles, config.EnvFiles); err != nil {
			log.Fatalf("Failed to deploy services: %v", err)
		}
		return
	}

	// Check if service exists
	containers, err := dockerClient.GetServiceContainers(ctx, config.Service, config.ComposeFiles, config.EnvFiles)
	log.Infof("CONTAINERS: %v", containers)
	if err != nil {
		log.Fatalf("Failed to get service containers: %v", err)
	}

	// If service doesn't exist, start it
	if len(containers) == 0 {
		log.Infof("Service '%s' is not running. Starting the service.", config.Service)
		if err := dockerClient.DeployService(ctx, config.Service, true, config.ComposeFiles, config.EnvFiles); err != nil {
			log.Fatalf("Failed to start service: %v", err)
		}
		return
	}

	// Get current containers before scaling
	oldContainers, err := dockerClient.GetServiceContainers(ctx, config.Service, config.ComposeFiles, config.EnvFiles)
	if err != nil {
		log.Fatalf("Failed to get current containers: %v", err)
	}

	scale := len(containers) * 2
	log.Infof("Scaling '%s' to '%d' instances", config.Service, scale)
	newContainers, err := dockerClient.ScaleService(ctx, config.Service, scale, config.ComposeFiles, config.EnvFiles)
	if err != nil {
		log.Fatalf("Failed to scale service: %v", err)
	}

	// Filter out old containers from newContainers
	var filteredNewContainers []string
	for _, newID := range newContainers {
		isOld := false
		for _, oldID := range oldContainers {
			if newID == oldID {
				isOld = true
				break
			}
		}
		if !isOld {
			filteredNewContainers = append(filteredNewContainers, newID)
		}
	}
	newContainers = filteredNewContainers

	log.Infof("Old containers: %v", oldContainers)
	log.Infof("New containers: %v", newContainers)
	// Wait for new containers to be healthy
	if err := dockerClient.WaitForHealthyContainers(ctx, newContainers, config.HealthcheckTimeout); err != nil {
		log.Errorf("New containers are not healthy. Rolling back.")
		if err := dockerClient.StopAndRemoveContainers(ctx, newContainers); err != nil {
			log.Errorf("Failed to rollback: %v", err)
		}
		os.Exit(1)
	}
	// Update Traefik configuration with new container IDs
	if err := traefik.UpdateConfig(log, oldContainers, newContainers, config.TraefikConfFile); err != nil {
		log.Warnf("Failed to update Traefik configuration: %v", err)
	}
	// Wait additional time if specified
	if config.WaitAfterHealthyDelay > 0 {
		log.Infof("Waiting for healthy containers to settle down (%s)", config.WaitAfterHealthyDelay)
		time.Sleep(config.WaitAfterHealthyDelay)
	}

	log.Infof("Waiting for %s before stopping old containers", config.NoHealthcheckTimeout)
	time.Sleep(config.NoHealthcheckTimeout)
	// Stop and remove old containers
	log.Infof("Stopping and removing old containers")
	if err := dockerClient.StopAndRemoveContainers(ctx, oldContainers); err != nil {
		log.Fatalf("Failed to stop and remove old containers: %v", err)
	}
}

// printPluginMetadata prints the Docker plugin metadata in JSON format
func printPluginMetadata() {
	metadata := PluginMetadata{
		SchemaVersion:    "0.1.0",
		Vendor:           "Serhii Kuianov",
		Version:          "v0.3",
		ShortDescription: "Docker plugin for docker-compose update with real zero time deployment.",
	}

	jsonData, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling metadata: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(jsonData))
}

func printUsage() {
	fmt.Println(`Usage: docker ztd [OPTIONS] SERVICE

Zero Time Deployment plugin for Docker Compose

Options:
  -t, --timeout SECONDS           Healthcheck timeout (default: 60)
  -w, --wait SECONDS             When no healthcheck is defined, wait for N seconds before stopping old container (default: 10)
  -wa, --wait-after-healthy SECONDS
                                  When healthcheck is defined and succeeds, wait for additional N seconds before stopping the old container (default: 0)
  -tc, --traefik-conf FILE       Specify Traefik configuration file (default: "traefik/dynamic_conf.yml")
  -p, --proxy TYPE               Set proxy type (traefik, nginx-proxy(not implemented yet)) (default: "traefik")
  -f, --file FILE                Compose configuration files (can be specified multiple times)
  -e, --env-file FILE            Environment files (can be specified multiple times)
  -h, --help                     Show this help message

Examples:
  docker ztd --timeout 120 -f docker-compose.yml api
  docker ztd -t 120 -f docker-compose.yml -e .env api
  docker ztd --wait-after-healthy 30 -f docker-compose.yml -e .env api
  docker ztd --traefik-conf custom/traefik.yml -f docker-compose.yml api`)
}

func parseArgs(log *logrus.Logger) *Config {
	config := &Config{
		// Default values
		HealthcheckTimeout:    60 * time.Second,
		NoHealthcheckTimeout:  10 * time.Second,
		WaitAfterHealthyDelay: 0 * time.Second,
		TraefikConfFile:       "traefik/dynamic_conf.yml",
		ProxyType:             "traefik",
	}

	// Skip the first two arguments (docker and ztd)
	args := os.Args[2:]

	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch {
		case arg == "--help" || arg == "-h":
			printUsage()
			os.Exit(0)

		case arg == "--timeout" || arg == "-t":
			if i+1 >= len(args) {
				log.Fatal("Missing value for --timeout flag")
			}
			duration, err := time.ParseDuration(args[i+1] + "s")
			if err != nil {
				log.Fatalf("Invalid timeout value: %v", err)
			}
			config.HealthcheckTimeout = duration
			i++

		case arg == "--wait" || arg == "-w":
			if i+1 >= len(args) {
				log.Fatal("Missing value for --wait flag")
			}
			duration, err := time.ParseDuration(args[i+1] + "s")
			if err != nil {
				log.Fatalf("Invalid wait value: %v", err)
			}
			config.NoHealthcheckTimeout = duration
			i++

		case arg == "--wait-after-healthy" || arg == "-wa":
			if i+1 >= len(args) {
				log.Fatal("Missing value for --wait-after-healthy flag")
			}
			duration, err := time.ParseDuration(args[i+1] + "s")
			if err != nil {
				log.Fatalf("Invalid wait-after-healthy value: %v", err)
			}
			config.WaitAfterHealthyDelay = duration
			i++

		case arg == "--traefik-conf" || arg == "-tc":
			if i+1 >= len(args) {
				log.Fatal("Missing value for --traefik-conf flag")
			}
			config.TraefikConfFile = args[i+1]
			i++

		case arg == "--proxy" || arg == "-p":
			if i+1 >= len(args) {
				log.Fatal("Missing value for --proxy flag")
			}
			config.ProxyType = args[i+1]
			i++

		case arg == "--file" || arg == "-f":
			if i+1 >= len(args) {
				log.Fatal("Missing value for --file flag")
			}
			config.ComposeFiles = append(config.ComposeFiles, args[i+1])
			i++

		case arg == "--env-file" || arg == "-e":
			if i+1 >= len(args) {
				log.Fatal("Missing value for --env-file flag")
			}
			config.EnvFiles = append(config.EnvFiles, args[i+1])
			i++

		case strings.HasPrefix(arg, "-"):
			log.Fatalf("Unknown flag: %s", arg)

		default:
			// This is the service name (positional argument)
			if config.Service != "" {
				log.Fatal("Service name can only be specified once")
			}
			config.Service = arg
		}
	}

	// Validate proxy type
	if config.ProxyType != "traefik" && config.ProxyType != "nginx-proxy" {
		log.Fatalf("Invalid proxy type: %s. Must be either 'traefik' or 'nginx-proxy'", config.ProxyType)
	}

	return config
}

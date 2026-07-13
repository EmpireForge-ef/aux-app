// Command server runs the Aux backend: a web frontend plus an AI agent that
// drives the full Spotify Web API on the user's behalf.
package main

import (
	"fmt"
	"os"

	// Embed the IANA timezone database so the configurable timezone works on
	// any base image (Alpine and scratch/distroless ship without tzdata) and
	// for bare-binary deploys, without depending on the host.
	_ "time/tzdata"

	"github.com/spf13/cobra"

	"github.com/EmpireForge-ef/aux-app/internal/config"
	"github.com/EmpireForge-ef/aux-app/internal/server"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var cfgFile string

	root := &cobra.Command{
		Use:           "aux",
		Short:         "Aux — AI-driven Spotify control with a web frontend",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ./aux.yaml or /etc/aux/aux.yaml)")

	serve := &cobra.Command{
		Use:   "serve",
		Short: "Start the HTTP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.New(cfgFile, cmd.Flags())
			if err != nil {
				return err
			}
			return server.Run(cmd.Context(), cfg, version)
		},
	}
	serve.Flags().String("addr", ":8080", "listen address")
	serve.Flags().String("static-dir", "", "directory with the built frontend (overrides static_dir)")

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version)
		},
	}

	root.AddCommand(serve, versionCmd)
	// Running the binary with no subcommand starts the server, which keeps
	// the container entrypoint trivial.
	root.RunE = serve.RunE
	root.Flags().AddFlagSet(serve.Flags())
	return root
}

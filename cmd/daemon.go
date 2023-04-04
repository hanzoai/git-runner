// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/mattn/go-isatty"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"gitea.com/gitea/act_runner/artifactcache"
	"gitea.com/gitea/act_runner/client"
	"gitea.com/gitea/act_runner/config"
	"gitea.com/gitea/act_runner/engine"
	"gitea.com/gitea/act_runner/poller"
	"gitea.com/gitea/act_runner/runtime"
)

func runDaemon(ctx context.Context, configFile *string) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		log.Infoln("Starting runner daemon")

		cfg, err := config.LoadDefault(*configFile)
		if err != nil {
			return fmt.Errorf("invalid configuration: %w", err)
		}

		initLogging(cfg)

		reg, err := config.LoadRegistration(cfg.Runner.File)
		if os.IsNotExist(err) {
			log.Error("registration file not found, please register the runner first")
			return err
		} else if err != nil {
			return fmt.Errorf("failed to load registration file: %w", err)
		}

		// require docker if a runner label uses a docker backend
		needsDocker := false
		for _, l := range reg.Labels {
			_, schema, _, _ := runtime.ParseLabel(l)
			if schema == "docker" {
				needsDocker = true
				break
			}
		}

		if needsDocker {
			// try to connect to docker daemon
			// if failed, exit with error
			if err := engine.Start(ctx); err != nil {
				log.WithError(err).Fatalln("failed to connect docker daemon engine")
			}
		}

		var g errgroup.Group

		cli := client.New(
			reg.Address,
			cfg.Runner.Insecure,
			reg.UUID,
			reg.Token,
			version,
		)

		runner := &runtime.Runner{
			Client:        cli,
			Machine:       reg.Name,
			ForgeInstance: reg.Address,
			Environ:       cfg.Runner.Envs,
			Labels:        reg.Labels,
			Network:       cfg.Container.Network,
			Version:       version,
		}

		if *cfg.Cache.Enabled {
			if handler, err := artifactcache.NewHandler(cfg.Cache.Dir, cfg.Cache.Host, cfg.Cache.Port); err != nil {
				log.Errorf("cannot init cache server, it will be disabled: %v", err)
			} else {
				log.Infof("cache handler listens on: %v", handler.ExternalURL())
				runner.CacheHandler = handler
			}
		}

		poller := poller.New(
			cli,
			runner.Run,
			cfg,
		)

		g.Go(func() error {
			l := log.WithField("capacity", cfg.Runner.Capacity).
				WithField("endpoint", reg.Address)
			l.Infoln("polling the remote server")

			if err := poller.Poll(ctx); err != nil {
				l.Errorf("poller error: %v", err)
			}
			poller.Wait()
			return nil
		})

		err = g.Wait()
		if err != nil {
			log.WithError(err).
				Errorln("shutting down the server")
		}
		return err
	}
}

// initLogging setup the global logrus logger.
func initLogging(cfg *config.Config) {
	isTerm := isatty.IsTerminal(os.Stdout.Fd())
	log.SetFormatter(&log.TextFormatter{
		DisableColors: !isTerm,
		FullTimestamp: true,
	})

	if l := cfg.Log.Level; l != "" {
		level, err := log.ParseLevel(l)
		if err != nil {
			log.WithError(err).
				Errorf("invalid log level: %q", l)
		}
		if log.GetLevel() != level {
			log.Infof("log level changed to %v", level)
			log.SetLevel(level)
		}
	}
}

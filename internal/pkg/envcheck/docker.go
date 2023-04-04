// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package envcheck

import (
	"context"
	"fmt"

	"github.com/docker/docker/client"
)

func CheckIfDockerRunning(ctx context.Context) error {
	// TODO: if runner support configures to use docker, we need config.Config to pass in
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return err
	}
	defer cli.Close()

	_, err = cli.Ping(ctx)
	if err != nil {
		return fmt.Errorf("cannot ping the docker daemon, does it running? %w", err)
	}

	return nil
}

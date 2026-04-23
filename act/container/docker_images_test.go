// Copyright 2022 The Gitea Authors. All rights reserved.
// Copyright 2020 The nektos/act Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package container

import (
	"context"
	"io"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func init() {
	log.SetLevel(log.DebugLevel)
}

func TestImageExistsLocally(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	ctx := context.Background()
	// to help make this test reliable and not flaky, we need to have
	// an image that will exist, and onew that won't exist

	// Test if image exists with specific tag
	invalidImageTag, err := ImageExistsLocally(ctx, "library/alpine:this-random-tag-will-never-exist", "linux/amd64")
	assert.Nil(t, err)                      //nolint:testifylint // pre-existing issue from nektos/act
	assert.Equal(t, false, invalidImageTag) //nolint:testifylint // pre-existing issue from nektos/act

	// Test if image exists with specific architecture (image platform)
	invalidImagePlatform, err := ImageExistsLocally(ctx, "alpine:latest", "windows/amd64")
	assert.Nil(t, err)                           //nolint:testifylint // pre-existing issue from nektos/act
	assert.Equal(t, false, invalidImagePlatform) //nolint:testifylint // pre-existing issue from nektos/act

	// pull an image
	cli, err := client.NewClientWithOpts(client.FromEnv)
	assert.Nil(t, err) //nolint:testifylint // pre-existing issue from nektos/act
	cli.NegotiateAPIVersion(context.Background())

	// Chose alpine latest because it's so small
	// maybe we should build an image instead so that tests aren't reliable on dockerhub
	readerDefault, err := cli.ImagePull(ctx, "node:16-buster-slim", types.ImagePullOptions{
		Platform: "linux/amd64",
	})
	assert.Nil(t, err) //nolint:testifylint // pre-existing issue from nektos/act
	defer readerDefault.Close()
	_, err = io.ReadAll(readerDefault)
	assert.Nil(t, err) //nolint:testifylint // pre-existing issue from nektos/act

	imageDefaultArchExists, err := ImageExistsLocally(ctx, "node:16-buster-slim", "linux/amd64")
	assert.Nil(t, err)                            //nolint:testifylint // pre-existing issue from nektos/act
	assert.Equal(t, true, imageDefaultArchExists) //nolint:testifylint // pre-existing issue from nektos/act

	// Validate if another architecture platform can be pulled
	readerArm64, err := cli.ImagePull(ctx, "node:16-buster-slim", types.ImagePullOptions{
		Platform: "linux/arm64",
	})
	assert.Nil(t, err) //nolint:testifylint // pre-existing issue from nektos/act
	defer readerArm64.Close()
	_, err = io.ReadAll(readerArm64)
	assert.Nil(t, err) //nolint:testifylint // pre-existing issue from nektos/act

	imageArm64Exists, err := ImageExistsLocally(ctx, "node:16-buster-slim", "linux/arm64")
	assert.Nil(t, err)                      //nolint:testifylint // pre-existing issue from nektos/act
	assert.Equal(t, true, imageArm64Exists) //nolint:testifylint // pre-existing issue from nektos/act
}

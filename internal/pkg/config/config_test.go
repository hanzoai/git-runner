// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadDefault_RejectsExternalServerWithoutSecret(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
cache:
  enabled: true
  external_server: "http://cache.invalid/"
`), 0o600))

	_, err := LoadDefault(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "external_secret")
}

func TestLoadDefault_AcceptsExternalServerWithSecret(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
cache:
  enabled: true
  external_server: "http://cache.invalid/"
  external_secret: "shh"
`), 0o600))

	_, err := LoadDefault(path)
	require.NoError(t, err)
}

func TestLoadDefault_TheEnvironmentSetsHowTheForgeIsPolled(t *testing.T) {
	t.Setenv("RUNNER_FETCH_TIMEOUT", "45s")
	t.Setenv("RUNNER_FETCH_INTERVAL", "8s")
	t.Setenv("RUNNER_FETCH_INTERVAL_MAX", "20s")

	cfg, err := LoadDefault("")
	require.NoError(t, err)
	assert.Equal(t, 45*time.Second, cfg.Runner.FetchTimeout)
	assert.Equal(t, 8*time.Second, cfg.Runner.FetchInterval)
	assert.Equal(t, 20*time.Second, cfg.Runner.FetchIntervalMax)
}

func TestLoadDefault_WithNothingSetThePollingIsTheDefault(t *testing.T) {
	cfg, err := LoadDefault("")
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, cfg.Runner.FetchTimeout)
	assert.Equal(t, 2*time.Second, cfg.Runner.FetchInterval)
}

func TestLoadDefault_RefusesAnUnreadableFetchSetting(t *testing.T) {
	for _, v := range []string{"soon", "0s", "-5s"} {
		t.Setenv("RUNNER_FETCH_TIMEOUT", v)
		_, err := LoadDefault("")
		require.Error(t, err, v)
		assert.Contains(t, err.Error(), "RUNNER_FETCH_TIMEOUT")
	}
}

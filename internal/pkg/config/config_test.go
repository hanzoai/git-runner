// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"path/filepath"
	"testing"

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

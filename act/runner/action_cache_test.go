// Copyright 2023 The Gitea Authors. All rights reserved.
// Copyright 2023 The nektos/act Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestActionCache(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	a := assert.New(t)
	cache := &GoGitActionCache{
		Path: t.TempDir(),
	}
	ctx := context.Background()
	cacheDir := "nektos/act-test-actions"
	repo := "https://github.com/nektos/act-test-actions"
	refs := []struct {
		Name     string
		CacheDir string
		Repo     string
		Ref      string
	}{
		{
			Name:     "Fetch Branch Name",
			CacheDir: cacheDir,
			Repo:     repo,
			Ref:      "main",
		},
		{
			Name:     "Fetch Branch Name Absolutely",
			CacheDir: cacheDir,
			Repo:     repo,
			Ref:      "refs/heads/main",
		},
		{
			Name:     "Fetch HEAD",
			CacheDir: cacheDir,
			Repo:     repo,
			Ref:      "HEAD",
		},
		{
			Name:     "Fetch Sha",
			CacheDir: cacheDir,
			Repo:     repo,
			Ref:      "de984ca37e4df4cb9fd9256435a3b82c4a2662b1",
		},
	}
	for _, c := range refs {
		t.Run(c.Name, func(t *testing.T) {
			sha, err := cache.Fetch(ctx, c.CacheDir, c.Repo, c.Ref, "")
			if !a.NoError(err) || !a.NotEmpty(sha) { //nolint:testifylint // pre-existing issue from nektos/act
				return
			}
			atar, err := cache.GetTarArchive(ctx, c.CacheDir, sha, "js")
			if !a.NoError(err) || !a.NotEmpty(atar) { //nolint:testifylint // pre-existing issue from nektos/act
				return
			}
			mytar := tar.NewReader(atar)
			th, err := mytar.Next()
			if !a.NoError(err) || !a.NotEqual(0, th.Size) { //nolint:testifylint // pre-existing issue from nektos/act
				return
			}
			buf := &bytes.Buffer{}
			// G110: Potential DoS vulnerability via decompression bomb (gosec)
			_, err = io.Copy(buf, mytar)
			a.NoError(err) //nolint:testifylint // pre-existing issue from nektos/act
			str := buf.String()
			a.NotEmpty(str)
		})
	}
}

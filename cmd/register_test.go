// Copyright 2022 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package cmd

import "testing"

func TestValidateLabels(t *testing.T) {
	labels := []string{"ubuntu-latest:docker://node:16-buster", "self-hosted"}
	if err := validateLabels(labels); err != nil {
		t.Errorf("validateLabels() error = %v", err)
	}
}

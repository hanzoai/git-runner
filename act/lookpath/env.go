// Copyright 2022 The Gitea Authors. All rights reserved.
// Copyright 2022 The nektos/act Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package lookpath

import "os"

type Env interface {
	Getenv(name string) string
}

type defaultEnv struct{}

func (*defaultEnv) Getenv(name string) string {
	return os.Getenv(name)
}

func LookPath(file string) (string, error) {
	return LookPath2(file, &defaultEnv{})
}

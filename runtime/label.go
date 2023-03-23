// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package runtime

import (
	"fmt"
	"strings"
)

func ParseLabel(str string) (label, schema, arg string, err error) {
	splits := strings.SplitN(str, ":", 3)
	label = splits[0]
	schema = "host"
	arg = ""
	if len(splits) >= 2 {
		schema = splits[1]
	}
	if len(splits) >= 3 {
		arg = splits[2]
	}
	if schema != "host" && schema != "docker" {
		return "", "", "", fmt.Errorf("unsupported schema: %s", schema)
	}
	return
}

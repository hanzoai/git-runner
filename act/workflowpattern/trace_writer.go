// Copyright 2023 The Gitea Authors. All rights reserved.
// Copyright 2023 The nektos/act Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package workflowpattern

import "fmt"

type TraceWriter interface {
	Info(string, ...any)
}

type EmptyTraceWriter struct{}

func (*EmptyTraceWriter) Info(string, ...any) {
}

type StdOutTraceWriter struct{}

func (*StdOutTraceWriter) Info(format string, args ...any) {
	fmt.Printf(format+"\n", args...) //nolint:forbidigo // pre-existing issue from nektos/act
}

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
	fmt.Printf(format+"\n", args...)
}

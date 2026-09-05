package app

import (
	"connectrpc.com/connect"
)

type uiInterceptor struct{ a *App }

func (i uiInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return i.a.interceptUI(next)
}

func (i uiInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i uiInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return i.a.interceptUIStream(next)
}

type workerInterceptor struct{ a *App }

func (i workerInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return i.a.interceptWorker(next)
}

func (i workerInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i workerInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return i.a.interceptWorkerStream(next)
}

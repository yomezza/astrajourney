package planetserver

import "context"

// Planet has an HTTP authentication phase followed by a long-lived hijacked
// WebSocket. The HTTP request context is not a reliable lifetime for the latter.
// Retain context values but not request cancellation or a deadline. The service
// lifecycle still tears down all sockets on shutdown.
func planetWebSocketContext(requestCtx, serviceCtx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.WithoutCancel(requestCtx))
	stop := context.AfterFunc(serviceCtx, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

package tui

import (
	"context"

	"github.com/conglinyizhi/sylastra/internal/agent"
)

// NewRuntimeAdapter wraps an *agent.Runtime into a tui.Runtime.
func NewRuntimeAdapter(rt *agent.Runtime) Runtime {
	return &agentRuntimeAdapter{rt: rt}
}

type agentRuntimeAdapter struct {
	rt *agent.Runtime
}

func (a *agentRuntimeAdapter) RunTurn(ctx context.Context, sess *agent.Session, input string, sink agent.Sink) error {
	return a.rt.RunTurn(ctx, sess, input, sink)
}

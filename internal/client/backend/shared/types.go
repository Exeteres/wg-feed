package shared

import (
	"context"

	"github.com/exeteres/wg-feed/internal/client/state"
	"github.com/exeteres/wg-feed/internal/model"
)

// ResolvedTunnel carries backend-ready effective state while preserving
// original server tunnel fields in the embedded model tunnel.
type ResolvedTunnel struct {
	model.Tunnel
	EffectiveEnabled bool
}

type Backend interface {
	Apply(ctx context.Context, tunnel ResolvedTunnel, state state.TunnelState) (state.TunnelState, error)
	Remove(ctx context.Context, state state.TunnelState) error
}

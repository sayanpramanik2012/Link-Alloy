package engine

import (
	"context"
	"errors"
)

var ErrElevationRequired = errors.New("Link Alloy needs to run as administrator to install system routes")

type RouteManager interface {
	Recover(context.Context) error
	Install(context.Context, TunnelDevice) error
	Stop(context.Context) error
}

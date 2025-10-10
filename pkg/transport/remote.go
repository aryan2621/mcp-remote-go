package transport

import (
	"context"
)

type RemoteTransport interface {
	Connect(ctx context.Context) error
	Send(ctx context.Context, data []byte) error
	Receive(ctx context.Context) ([]byte, error)
	Close() error
}

type SessionAwareTransport interface {
	RemoteTransport
	GetSessionID() string
	TerminateSession(ctx context.Context) error
}
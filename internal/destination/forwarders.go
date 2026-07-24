package destination

import (
	"context"

	"github.com/gps-data-receiver/internal/hooshnics"
	"github.com/gps-data-receiver/internal/sender"
)

// Forwarder is a thin destination adapter. Retry / circuit breaker / DLQ live in the
// underlying clients and Redis retry consumers — not reimplemented here.
type Forwarder interface {
	Name() string
	SendParsed(ctx context.Context, payload []byte) error
}

// PiStatForwarder wraps HTTPSender (DESTINATION_SERVERS).
type PiStatForwarder struct {
	Sender *sender.HTTPSender
}

func (p PiStatForwarder) Name() string { return "pistat" }

func (p PiStatForwarder) SendParsed(ctx context.Context, payload []byte) error {
	if p.Sender == nil {
		return nil
	}
	res := p.Sender.Send(ctx, payload)
	if res.Success {
		return nil
	}
	return res.Error
}

// HooshnicsParsedForwarder wraps the async Hooshnics mirror (parsed endpoint).
type HooshnicsParsedForwarder struct {
	Mirror *hooshnics.Forwarder
}

func (h HooshnicsParsedForwarder) Name() string { return "hooshnics" }

func (h HooshnicsParsedForwarder) SendParsed(_ context.Context, payload []byte) error {
	if h.Mirror == nil {
		return nil
	}
	h.Mirror.ForwardParsed(payload)
	return nil
}

package enskit

import (
	"context"
	"log/slog"
	"time"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// WSHeads streams heads over an eth_subscribe("newHeads") websocket and
// transparently reconnects. While disconnected no heads flow, so the
// Watcher's MaxHeadAge fail-closed timer covers outages.
type WSHeads struct {
	URL string
	Log *slog.Logger
}

func (s WSHeads) Heads(ctx context.Context) (<-chan Head, error) {
	out := make(chan Head, 16)
	log := s.Log
	if log == nil {
		log = slog.Default()
	}
	go func() {
		defer close(out)
		backoff := time.Second
		for ctx.Err() == nil {
			err := s.stream(ctx, out)
			if ctx.Err() != nil {
				return
			}
			log.Warn("head subscription dropped, reconnecting", "err", err, "in", backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 10*time.Second)
		}
	}()
	return out, nil
}

func (s WSHeads) stream(ctx context.Context, out chan<- Head) error {
	c, err := ethclient.DialContext(ctx, s.URL)
	if err != nil {
		return err
	}
	defer c.Close()
	ch := make(chan *types.Header, 16)
	sub, err := c.SubscribeNewHead(ctx, ch)
	if err != nil {
		return err
	}
	defer sub.Unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-sub.Err():
			return err
		case h := <-ch:
			select {
			case out <- Head{Number: h.Number, Time: h.Time, SeenAt: time.Now()}:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

// PollHeads polls eth_getBlockByNumber("latest") over HTTP. Useful for
// local anvil and RPCs without websockets.
type PollHeads struct {
	Client   *ethclient.Client
	Interval time.Duration // default 1s
}

func (p PollHeads) Heads(ctx context.Context) (<-chan Head, error) {
	out := make(chan Head, 16)
	iv := p.Interval
	if iv == 0 {
		iv = time.Second
	}
	go func() {
		defer close(out)
		var last uint64
		t := time.NewTicker(iv)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				h, err := p.Client.HeaderByNumber(ctx, nil)
				if err != nil || h.Number.Uint64() == last {
					continue
				}
				last = h.Number.Uint64()
				select {
				case out <- Head{Number: h.Number, Time: h.Time, SeenAt: time.Now()}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

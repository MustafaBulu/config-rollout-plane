package agent

import (
	"context"
	"math/rand/v2"
	"time"
)

type Syncer struct {
	Client       SnapshotClient
	Cache        *SnapshotCache
	Acknowledger Acknowledger
	PollInterval time.Duration
	etag         string
}

func (s *Syncer) SyncOnce(ctx context.Context) error {
	result, err := s.Client.Fetch(ctx, s.etag)
	if err != nil {
		return err
	}
	if !result.Changed {
		return nil
	}

	if err := s.Cache.Save(ctx, result.Snapshot); err != nil {
		return err
	}
	s.etag = result.ETag

	if s.Acknowledger != nil {
		for _, item := range result.Snapshot.Configs {
			if err := s.Acknowledger.Acknowledge(ctx, s.Client.AgentID, s.Client.Token, item, result.Snapshot.Revision); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *Syncer) Run(ctx context.Context) error {
	interval := s.PollInterval
	if interval == 0 {
		interval = 2 * time.Second
	}

	for {
		_ = s.SyncOnce(ctx)

		timer := time.NewTimer(jitter(interval))
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func jitter(interval time.Duration) time.Duration {
	delta := interval / 5
	if delta == 0 {
		return interval
	}
	offset := time.Duration(rand.Int64N(int64(delta)*2+1)) - delta
	return interval + offset
}

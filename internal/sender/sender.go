package sender

import (
	"context"
	"time"

	"github.com/pachmu/skyeng-push-notificator/internal/state"
	"github.com/sirupsen/logrus"
)

// NewSender returns Sender struct.
func NewSender(state *state.State) *Sender {
	return &Sender{
		state: state,
	}
}

// Sender represents periodic sender logic.
type Sender struct {
	state *state.State
}

// Run executes main application logic.
func (s *Sender) Run(ctx context.Context) error {
	ticker := time.NewTicker(s.state.GetTimeInterval())
	run := true
	suspended := false
	for run {
		if !suspended {
			err := s.state.WordsetCallback()
			if err != nil {
				logrus.Error(err)
			}
		}
		select {
		case timeInterval := <-s.state.GetChangeInterval():
			ticker.Stop()
			ticker = time.NewTicker(timeInterval)
			suspended = false
			continue
		case <-ticker.C:
			continue
		case <-s.state.GetSuspendWork():
			ticker.Stop()
			suspended = true
			continue
		case <-ctx.Done():
			ticker.Stop()
			run = false
			continue
		}

	}
	return nil
}

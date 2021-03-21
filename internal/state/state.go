package state

import (
	"sync"
	"time"
)

type State struct {
	timeInterval        time.Duration
	sendWordsetCallback func() error
	changeInterval      chan time.Duration
	suspend             chan struct{}
	mx                  sync.Mutex
}

func NewState(timeInterval time.Duration) *State {
	return &State{
		sendWordsetCallback: func() error {
			return nil
		},
		timeInterval:   timeInterval,
		changeInterval: make(chan time.Duration),
		suspend:        make(chan struct{}),
	}
}

func (s *State) GetTimeInterval() time.Duration {
	s.mx.Lock()
	defer s.mx.Unlock()
	return s.timeInterval
}

func (s *State) GetChangeInterval() <-chan time.Duration {
	return s.changeInterval
}

func (s *State) GetSuspendWork() <-chan struct{} {
	return s.suspend
}

func (s *State) WordsetCallback() error {
	s.mx.Lock()
	defer s.mx.Unlock()
	err := s.sendWordsetCallback()
	if err != nil {
		return err
	}
	return nil
}

// ChangeCurrentWordset changes sending wordset callback.
func (s *State) SetWordsetCallback(callback func() error) {
	s.mx.Lock()
	defer s.mx.Unlock()
	s.sendWordsetCallback = callback
}

// ChangeTimeInterval changes sending interval.
func (s *State) ChangeTimeInterval(interval time.Duration) {
	s.mx.Lock()
	defer s.mx.Unlock()
	s.changeInterval <- interval
}

// SuspendWork suspending sender.
func (s *State) SuspendWork() {
	s.suspend <- struct{}{}
}

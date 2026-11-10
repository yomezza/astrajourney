package chatserver

import (
	"context"
	"sync/atomic"
)

// WakeSource hosts A1/A2 and can host B1 later. Neither audience nor game state belongs
// here. Start must establish the source before the initial database reconcile.
type WakeSource interface {
	Start(context.Context) error
	Wakeups() <-chan struct{}
	AfterCommit()
	Healthy() bool
	Close(context.Context) error
}
type LocalWakeSource struct {
	wakes  chan struct{}
	closed atomic.Bool
}

func NewLocalWakeSource() *LocalWakeSource             { return &LocalWakeSource{wakes: make(chan struct{}, 1)} }
func (s *LocalWakeSource) Start(context.Context) error { return nil }
func (s *LocalWakeSource) Wakeups() <-chan struct{}    { return s.wakes }
func (s *LocalWakeSource) Healthy() bool               { return !s.closed.Load() }
func (s *LocalWakeSource) AfterCommit() {
	if !s.closed.Load() {
		select {
		case s.wakes <- struct{}{}:
		default:
		}
	}
}
func (s *LocalWakeSource) Close(context.Context) error { s.closed.Store(true); return nil }

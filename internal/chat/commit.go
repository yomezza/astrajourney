package chat

// ObserveCommits attaches one process-local, nonblocking wake source. The
// conditional unsubscribe cannot clear a later observer on a reused Service.
func (s *Service) ObserveCommits(notify func(string)) func() {
	hook := &notify
	s.commitWake.Store(hook)
	return func() { s.commitWake.CompareAndSwap(hook, nil) }
}
func (s *Service) committed(kind string) {
	if hook := s.commitWake.Load(); hook != nil {
		(*hook)(kind)
	}
}
func (s *Service) DispatchCounters() map[string]uint64 {
	return map[string]uint64{"changes_calls": s.changesCalls.Load(), "scope_checks": s.scopeChecks.Load()}
}

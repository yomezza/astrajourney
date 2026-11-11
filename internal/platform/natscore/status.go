package natscore

// Status is a credential-free transport snapshot, independent of game readiness.
type Status struct {
	State     string `json:"state"`
	Enabled   bool   `json:"enabled"`
	Connected bool   `json:"connected"`
	Mode      string `json:"mode,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

func StatusFromDiagnostics(d map[string]any) Status {
	enabled, _ := d["enabled"].(bool)
	healthy, _ := d["healthy"].(bool)
	s := Status{State: "disabled", Enabled: enabled, Connected: enabled && healthy}
	if !enabled {
		return s
	}
	if healthy {
		s.State = "connected"
		return s
	}
	s.State = "connecting"
	if attempts, _ := d["connect_attempts"].(uint64); attempts > 0 {
		s.State = "disconnected"
	}
	if code, _ := d["last_error_code"].(string); code != "" {
		switch code {
		case "network_unavailable", "authentication_failed", "authorization_failed", "tls_verification_failed", "slow_consumer":
			s.ErrorCode = code
		}
	}
	return s
}

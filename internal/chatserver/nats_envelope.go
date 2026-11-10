package chatserver

import (
	"bytes"
	"encoding/json"
	"io"
	"time"

	"github.com/yomezza/cosmosimulator/internal/chat"
)

const ChatOutboxSubject = "cosmos.v1.signal.chat.outbox.changed"
const maxNATSSignal = 2048

type chatSignal struct {
	SchemaVersion int       `json:"schema_version"`
	SignalID      string    `json:"signal_id,omitempty"`
	Source        string    `json:"source"`
	Kind          string    `json:"kind"`
	OccurredAt    time.Time `json:"occurred_at"`
	Data          struct {
		Hint string `json:"hint"`
	} `json:"data"`
}

func newChatSignal() []byte {
	s := chatSignal{SchemaVersion: 1, SignalID: chat.UUID(), Source: "chat", Kind: "chat.outbox.changed", OccurredAt: time.Now().UTC()}
	s.Data.Hint = "outbox"
	b, _ := json.Marshal(s)
	return b
}
func validSignalUUID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, c := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func decodeChatSignal(subject string, b []byte) (chatSignal, bool) {
	var s chatSignal
	if subject != ChatOutboxSubject || len(b) == 0 || len(b) > maxNATSSignal {
		return s, false
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&s) != nil || d.Decode(new(any)) != io.EOF {
		return s, false
	}
	_, offset := s.OccurredAt.Zone()
	return s, s.SchemaVersion == 1 && s.Source == "chat" && s.Kind == "chat.outbox.changed" && s.Data.Hint == "outbox" && !s.OccurredAt.IsZero() && offset == 0 && (s.SignalID == "" || validSignalUUID(s.SignalID))
}

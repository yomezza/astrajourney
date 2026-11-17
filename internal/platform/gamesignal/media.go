package gamesignal

import (
	"encoding/json"
	"github.com/yomezza/cosmosimulator/internal/platform/identifiers"
	"time"
)

const AssetsReadySubject = "cosmos.v1.signal.cdn.assets.ready"
const AssetsChangedSubject = "cosmos.v1.signal.cdn.assets.changed"
const MediaChangedSubject = "cosmos.v1.signal.cosmic.media.changed"

type MediaChange struct {
	ID    string `json:"media_id"`
	State string `json:"state"`
}
type AssetChange struct {
	Revision string `json:"revision"`
}

func EncodeMedia(id, state string) (Message, bool) {
	d := MediaChange{id, state}
	if !identifiers.ValidUUID(id) || (state != "ready" && state != "deleted" && state != "quarantined") {
		return Message{}, false
	}
	uuid, err := identifiers.UUID()
	if err != nil {
		return Message{}, false
	}
	b, err := json.Marshal(Envelope[MediaChange]{1, uuid, "cosmic", "cosmic.media.changed", time.Now().UTC(), d})
	return Message{MediaChangedSubject, b}, err == nil
}
func DecodeMedia(subject string, b []byte) bool {
	var e Envelope[MediaChange]
	return subject == MediaChangedSubject && strict(b, &e) && header(e, "cosmic", "cosmic.media.changed") && identifiers.ValidUUID(e.Data.ID) && (e.Data.State == "ready" || e.Data.State == "deleted" || e.Data.State == "quarantined")
}
func EncodeAssets(subject, revision string) (Message, bool) {
	if (subject != AssetsReadySubject && subject != AssetsChangedSubject) || len(revision) != 64 {
		return Message{}, false
	}
	for _, r := range revision {
		if !((r >= 'a' && r <= 'f') || (r >= '0' && r <= '9')) {
			return Message{}, false
		}
	}
	kind := "cdn.assets.ready"
	if subject == AssetsChangedSubject {
		kind = "cdn.assets.changed"
	}
	id, e := identifiers.UUID()
	if e != nil {
		return Message{}, false
	}
	b, e := json.Marshal(Envelope[AssetChange]{1, id, "cdn", kind, time.Now().UTC(), AssetChange{revision}})
	return Message{subject, b}, e == nil
}

func DecodeAssets(subject string, b []byte) bool {
	var e Envelope[AssetChange]
	kind := "cdn.assets.ready"
	if subject == AssetsChangedSubject {
		kind = "cdn.assets.changed"
	} else if subject != AssetsReadySubject {
		return false
	}
	if !strict(b, &e) || !header(e, "cdn", kind) || len(e.Data.Revision) != 64 {
		return false
	}
	for _, r := range e.Data.Revision {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}

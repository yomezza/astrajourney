package universestore

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
)

// PreserveLegacyExtras retains extension fields not represented by the current
// Go DTO. They belong to per-object details, never another whole-world blob.
func PreserveLegacyExtras(ctx context.Context, tx pgx.Tx, raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	var root map[string]json.RawMessage
	if e := json.Unmarshal(raw, &root); e != nil {
		return e
	}
	extras := func(m map[string]json.RawMessage, known ...string) []byte {
		copy := map[string]json.RawMessage{}
		for k, v := range m {
			copy[k] = v
		}
		for _, k := range known {
			delete(copy, k)
		}
		if len(copy) == 0 {
			return nil
		}
		return bytes(copy)
	}
	var stars []map[string]json.RawMessage
	if e := json.Unmarshal(root["stars"], &stars); e != nil {
		return e
	}
	batch := &pgx.Batch{}
	flush := func() error {
		if batch.Len() == 0 {
			return nil
		}
		r := tx.SendBatch(ctx, batch)
		defer r.Close()
		for range batch.Len() {
			tag, e := r.Exec()
			if e != nil {
				return e
			}
			if tag.RowsAffected() != 1 {
				return errors.New("legacy extension fields reference a removed object; migration needs review")
			}
		}
		batch = &pgx.Batch{}
		return r.Close()
	}
	for _, s := range stars {
		var star int
		if e := json.Unmarshal(s["id"], &star); e != nil {
			return e
		}
		data := extras(s, "uuid", "id", "name", "x", "y", "region", "class", "mass", "luminosity", "planets", "catalog_code", "discovery_seed", "resource_rolls", "deleted", "number", "founder", "ports", "resource_weights", "galaxy")
		if len(data) > 0 {
			batch.Queue(`UPDATE star_systems SET details=details||$2::jsonb WHERE id=$1`, star, data)
		}
		var planets []map[string]json.RawMessage
		if p := s["planets"]; len(p) > 0 {
			if e := json.Unmarshal(p, &planets); e != nil {
				return e
			}
		}
		for _, p := range planets {
			var id int
			if e := json.Unmarshal(p["id"], &id); e != nil {
				return e
			}
			data = extras(p, "uuid", "id", "name", "kind", "au", "mass", "gravity", "temperature", "atmosphere", "resources", "catalog_code")
			if len(data) > 0 {
				batch.Queue(`UPDATE system_planets SET details=details||$3::jsonb WHERE system_id=$1 AND id=$2`, star, id, data)
			}
		}
		if batch.Len() >= 512 {
			if e := flush(); e != nil {
				return e
			}
		}
	}
	if e := flush(); e != nil {
		return e
	}
	if data := extras(root, "discovery_secret", "settlement_version", "revision", "galaxies", "ring_layout_version", "naming_version", "name", "seed", "version", "stars", "regions", "lanes"); len(data) > 0 {
		_, e := tx.Exec(ctx, `UPDATE universe_state SET details=details||$1::jsonb WHERE id=1`, data)
		return e
	}
	return nil
}

package settings

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"sync"
)

const keyFormKey = "form_signing_key"

var formKey struct {
	mu  sync.Mutex
	key []byte
}

// FormKey is the key public forms sign their load time with, so a
// submission sent too soon after its page loaded can be told apart.
// Made on first use and kept in the settings table, so it holds across
// restarts and replicas.
func FormKey(ctx context.Context, db *sql.DB) ([]byte, error) {
	formKey.mu.Lock()
	defer formKey.mu.Unlock()
	if formKey.key != nil {
		return formKey.key, nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	// The first server to get here sets it; the others read it back.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO settings(key, value, updated_at) VALUES ($1, $2, now_text())
		ON CONFLICT(key) DO NOTHING
	`, keyFormKey, hex.EncodeToString(b)); err != nil {
		return nil, err
	}
	v, err := getString(ctx, db, keyFormKey)
	if err != nil {
		return nil, err
	}
	key, err := hex.DecodeString(v)
	if err != nil {
		return nil, err
	}
	if len(key) < 32 {
		return nil, errors.New("settings: form signing key too short")
	}
	formKey.key = key
	return key, nil
}

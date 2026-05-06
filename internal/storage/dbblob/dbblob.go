// Package dbblob is the storage.Blobs implementation that keeps
// everything in Postgres — the inline avatar_* columns on the teams
// and adventures tables that the app has always used. Default impl;
// no operator action required to enable. Future S3 / local-FS impls
// live in sibling packages and are selected via build tags.
package dbblob

import (
	"context"
	"database/sql"
	"errors"

	"git.sr.ht/~klahr/zv/internal/storage"
)

// Store implements storage.Blobs against the live SQL database.
type Store struct {
	DB *sql.DB
}

// New wires a Store to a database handle. The handle is held for the
// lifetime of the process; no shutdown work is required because the
// caller owns the *sql.DB.
func New(db *sql.DB) *Store {
	return &Store{DB: db}
}

func (s *Store) PutTeamAvatar(ctx context.Context, teamID int64, content []byte, mimeType string) error {
	res, err := s.DB.ExecContext(ctx, `
		UPDATE teams
		   SET avatar_mime_type = $1, avatar_size_bytes = $2, avatar_content = $3
		 WHERE id = $4
	`, mimeType, int64(len(content)), content, teamID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (s *Store) GetTeamAvatar(ctx context.Context, teamID int64) ([]byte, string, error) {
	var (
		mime    sql.NullString
		size    sql.NullInt64
		content []byte
	)
	err := s.DB.QueryRowContext(ctx,
		`SELECT avatar_mime_type, avatar_size_bytes, avatar_content FROM teams WHERE id = $1`, teamID).
		Scan(&mime, &size, &content)
	if errors.Is(err, sql.ErrNoRows) || !mime.Valid || len(content) == 0 {
		return nil, "", storage.ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	return content, mime.String, nil
}

func (s *Store) DeleteTeamAvatar(ctx context.Context, teamID int64) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE teams
		   SET avatar_mime_type = NULL, avatar_size_bytes = NULL, avatar_content = NULL
		 WHERE id = $1
	`, teamID)
	return err
}

func (s *Store) PutAdventureAvatar(ctx context.Context, slug string, content []byte, mimeType string) error {
	res, err := s.DB.ExecContext(ctx, `
		UPDATE adventures
		   SET avatar_mime_type = $1, avatar_size_bytes = $2, avatar_content = $3
		 WHERE slug = $4
	`, mimeType, int64(len(content)), content, slug)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (s *Store) GetAdventureAvatar(ctx context.Context, slug string) ([]byte, string, error) {
	var (
		mime    sql.NullString
		size    sql.NullInt64
		content []byte
	)
	err := s.DB.QueryRowContext(ctx, `
		SELECT avatar_mime_type, avatar_size_bytes, avatar_content
		FROM adventures WHERE slug = $1
	`, slug).Scan(&mime, &size, &content)
	if errors.Is(err, sql.ErrNoRows) || !mime.Valid || len(content) == 0 {
		return nil, "", storage.ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	return content, mime.String, nil
}

func (s *Store) DeleteAdventureAvatar(ctx context.Context, slug string) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE adventures
		   SET avatar_mime_type = NULL, avatar_size_bytes = NULL, avatar_content = NULL
		 WHERE slug = $1
	`, slug)
	return err
}

func (s *Store) PutOrgLogo(ctx context.Context, orgID int64, content []byte, mimeType string) error {
	res, err := s.DB.ExecContext(ctx, `
		UPDATE organizations
		   SET logo_mime_type = $1, logo_size_bytes = $2, logo_content = $3
		 WHERE id = $4
	`, mimeType, int64(len(content)), content, orgID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (s *Store) GetOrgLogo(ctx context.Context, orgID int64) ([]byte, string, error) {
	var (
		mime    sql.NullString
		size    sql.NullInt64
		content []byte
	)
	err := s.DB.QueryRowContext(ctx, `
		SELECT logo_mime_type, logo_size_bytes, logo_content
		FROM organizations WHERE id = $1
	`, orgID).Scan(&mime, &size, &content)
	if errors.Is(err, sql.ErrNoRows) || !mime.Valid || len(content) == 0 {
		return nil, "", storage.ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	return content, mime.String, nil
}

func (s *Store) DeleteOrgLogo(ctx context.Context, orgID int64) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE organizations
		   SET logo_mime_type = NULL, logo_size_bytes = NULL, logo_content = NULL
		 WHERE id = $1
	`, orgID)
	return err
}

func (s *Store) PutEventCover(ctx context.Context, eventID int64, content []byte, mimeType string) error {
	res, err := s.DB.ExecContext(ctx, `
		UPDATE events
		   SET cover_image_mime_type = $1, cover_image_size_bytes = $2, cover_image_content = $3
		 WHERE id = $4
	`, mimeType, int64(len(content)), content, eventID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (s *Store) GetEventCover(ctx context.Context, eventID int64) ([]byte, string, error) {
	var (
		mime    sql.NullString
		size    sql.NullInt64
		content []byte
	)
	err := s.DB.QueryRowContext(ctx, `
		SELECT cover_image_mime_type, cover_image_size_bytes, cover_image_content
		FROM events WHERE id = $1
	`, eventID).Scan(&mime, &size, &content)
	if errors.Is(err, sql.ErrNoRows) || !mime.Valid || len(content) == 0 {
		return nil, "", storage.ErrNotFound
	}
	if err != nil {
		return nil, "", err
	}
	return content, mime.String, nil
}

func (s *Store) DeleteEventCover(ctx context.Context, eventID int64) error {
	_, err := s.DB.ExecContext(ctx, `
		UPDATE events
		   SET cover_image_mime_type = NULL, cover_image_size_bytes = NULL, cover_image_content = NULL
		 WHERE id = $1
	`, eventID)
	return err
}

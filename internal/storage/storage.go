// Package storage is the read/write surface for the binary assets the
// app owns — team avatars and adventure cover art today; page images
// follow when their slightly-larger metadata model joins the
// abstraction. dbblob (everything in Postgres, today's behaviour) is
// the default implementation; future build-tag impls (localfs, s3)
// plug in next to it and are selected at compile time, with the
// Settings → Installed components panel reflecting the choice.
//
// Scope on purpose:
//
//   - Bytes + MIME in, bytes + MIME out. Image shrinking and MIME
//     validation stay in the handler so swapping storage doesn't
//     re-implement those.
//   - Domain-specific method names (PutTeamAvatar, …) rather than a
//     generic Put(bucket, key, …) — the entity tables already differ
//     in shape, and a future S3 impl just translates (entity, id) to
//     an object key.
package storage

import (
	"context"
	"errors"
)

// ErrNotFound is returned by Get/Put/Delete when the addressed entity
// row doesn't exist (Get also returns it when the entity exists but
// has no avatar stored). Wrapped, so callers should compare with
// errors.Is.
var ErrNotFound = errors.New("storage: not found")

// Blobs is the seam wired into the admin and public handlers.
// Every method here owns one entity's avatar; helpers that span
// entities live on top, in the caller.
type Blobs interface {
	PutTeamAvatar(ctx context.Context, teamID int64, content []byte, mimeType string) error
	GetTeamAvatar(ctx context.Context, teamID int64) (content []byte, mimeType string, err error)
	DeleteTeamAvatar(ctx context.Context, teamID int64) error

	PutAdventureAvatar(ctx context.Context, slug string, content []byte, mimeType string) error
	GetAdventureAvatar(ctx context.Context, slug string) (content []byte, mimeType string, err error)
	DeleteAdventureAvatar(ctx context.Context, slug string) error

	PutOrgLogo(ctx context.Context, orgID int64, content []byte, mimeType string) error
	GetOrgLogo(ctx context.Context, orgID int64) (content []byte, mimeType string, err error)
	DeleteOrgLogo(ctx context.Context, orgID int64) error

	PutEventCover(ctx context.Context, eventID int64, content []byte, mimeType string) error
	GetEventCover(ctx context.Context, eventID int64) (content []byte, mimeType string, err error)
	DeleteEventCover(ctx context.Context, eventID int64) error
}

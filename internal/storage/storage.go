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
	"fmt"
)

// ErrNotFound is the umbrella sentinel for "blob unavailable" — the
// addressed entity row doesn't exist OR the row exists but has no
// avatar stored. Existing callers compare with errors.Is(err,
// ErrNotFound) and treat both cases identically, which is fine for
// the avatar/cover serving paths we have today (404 fallback either
// way).
//
// New code that needs to distinguish "team doesn't exist" from "team
// has no avatar yet" can compare against ErrEntityMissing or
// ErrNoBlob directly. Both wrap ErrNotFound, so legacy callers
// continue to work.
var (
	ErrNotFound      = errors.New("storage: not found")
	ErrEntityMissing = fmt.Errorf("storage: entity missing: %w", ErrNotFound)
	ErrNoBlob        = fmt.Errorf("storage: no blob: %w", ErrNotFound)
)

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

	PutUserAvatar(ctx context.Context, userID int64, content []byte, mimeType string) error
	GetUserAvatar(ctx context.Context, userID int64) (content []byte, mimeType string, err error)
	DeleteUserAvatar(ctx context.Context, userID int64) error

	PutOrgLogo(ctx context.Context, orgID int64, content []byte, mimeType string) error
	GetOrgLogo(ctx context.Context, orgID int64) (content []byte, mimeType string, err error)
	DeleteOrgLogo(ctx context.Context, orgID int64) error

	PutOrgCover(ctx context.Context, orgID int64, content []byte, mimeType string) error
	GetOrgCover(ctx context.Context, orgID int64) (content []byte, mimeType string, err error)
	DeleteOrgCover(ctx context.Context, orgID int64) error

	PutEventCover(ctx context.Context, eventID int64, content []byte, mimeType string) error
	GetEventCover(ctx context.Context, eventID int64) (content []byte, mimeType string, err error)
	DeleteEventCover(ctx context.Context, eventID int64) error
}

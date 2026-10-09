// Package eventfile stores the files an event's organisers hand its
// players ahead of the start: a PDF with the rules, a map to print, a
// packing list. Like the organisers' messages (eventmsg) they sit beside
// the game, never in the adventure, and every team of the event sees
// them all.
package eventfile

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxSize is the largest file, in bytes: a long PDF with pictures.
	MaxSize = 20 << 20
	// MaxFiles is how many files one event may have.
	MaxFiles = 10
	// maxName is the longest file name kept, in bytes.
	maxName = 200
)

// File is one file, without its bytes.
type File struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Mime      string    `json:"mime"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

var (
	ErrEmpty       = errors.New("the file is empty")
	ErrTooLarge    = fmt.Errorf("a file is at most %d MB", MaxSize>>20)
	ErrTooMany     = fmt.Errorf("an event has at most %d files", MaxFiles)
	ErrUnsupported = errors.New("only PDF, pictures, text and Office documents are accepted")
	ErrNotFound    = errors.New("no such file in this event")
)

type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// officeTypes are the Office documents taken by their name's extension:
// their bytes sniff as a plain zip.
var officeTypes = map[string]string{
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".odt":  "application/vnd.oasis.opendocument.text",
	".ods":  "application/vnd.oasis.opendocument.spreadsheet",
	".odp":  "application/vnd.oasis.opendocument.presentation",
}

// TypeOf is the type a file is stored and served as, from its bytes and,
// for Office documents, its name; ErrUnsupported for any other kind.
func TypeOf(name string, content []byte) (string, error) {
	sniffed, _, _ := strings.Cut(http.DetectContentType(content), ";")
	switch sniffed {
	case "application/pdf", "image/png", "image/jpeg", "image/gif", "image/webp":
		return sniffed, nil
	case "text/plain":
		return "text/plain; charset=utf-8", nil
	case "application/zip":
		if t, ok := officeTypes[strings.ToLower(path.Ext(name))]; ok {
			return t, nil
		}
	}
	return "", ErrUnsupported
}

// CleanName is a file name fit to show and to download as: no folders,
// no control characters, valid UTF-8, at most maxName bytes with its
// extension kept.
func CleanName(name string) string {
	name = strings.ToValidUTF8(name, "")
	name = path.Base(strings.ReplaceAll(name, `\`, "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '"' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if strings.Trim(name, "./") == "" {
		return "file"
	}
	if len(name) > maxName {
		ext := path.Ext(name)
		if len(ext) > 16 {
			ext = ""
		}
		stem := name[:maxName-len(ext)]
		for !utf8.ValidString(stem) {
			stem = stem[:len(stem)-1]
		}
		name = stem + ext
	}
	return name
}

// Add stores a file for the event's players. The name is cleaned and the
// type found from the bytes (TypeOf).
func Add(ctx context.Context, q Querier, eventID, userID int64, name string, content []byte) (File, error) {
	switch {
	case len(content) == 0:
		return File{}, ErrEmpty
	case len(content) > MaxSize:
		return File{}, ErrTooLarge
	}
	name = CleanName(name)
	mime, err := TypeOf(name, content)
	if err != nil {
		return File{}, err
	}
	var by any
	if userID > 0 {
		by = userID
	}
	// The count and the insert in one statement, so two uploads at once
	// can't both take the last place.
	f := File{Name: name, Mime: mime, Size: int64(len(content))}
	err = q.QueryRowContext(ctx, `
		INSERT INTO event_files (event_id, name, mime_type, size_bytes, content, created_by)
		SELECT $1, $2, $3, $4, $5, $6
		WHERE (SELECT count(*) FROM event_files WHERE event_id = $1) < $7
		RETURNING id, created_at`, eventID, name, mime, f.Size, content, by, MaxFiles).Scan(&f.ID, &f.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return File{}, ErrTooMany
	}
	return f, err
}

// Delete removes one of the event's files.
func Delete(ctx context.Context, q Querier, eventID, id int64) error {
	res, err := q.ExecContext(ctx, `DELETE FROM event_files WHERE event_id = $1 AND id = $2`, eventID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// List is every file of the event, oldest first: the order they were
// added in, which is usually the order to read them in.
func List(ctx context.Context, q Querier, eventID int64) ([]File, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, name, mime_type, size_bytes, created_at
		FROM event_files WHERE event_id = $1 ORDER BY created_at, id`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []File{}
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.ID, &f.Name, &f.Mime, &f.Size, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Get is one of the event's files with its bytes.
func Get(ctx context.Context, q Querier, eventID, id int64) (File, []byte, error) {
	var (
		f       File
		content []byte
	)
	err := q.QueryRowContext(ctx, `
		SELECT id, name, mime_type, size_bytes, created_at, content
		FROM event_files WHERE event_id = $1 AND id = $2`, eventID, id).
		Scan(&f.ID, &f.Name, &f.Mime, &f.Size, &f.CreatedAt, &content)
	if errors.Is(err, sql.ErrNoRows) {
		return File{}, nil, ErrNotFound
	}
	return f, content, err
}

// Serve writes a file as a download. It is always an attachment, never
// shown in the page, so an uploaded file can't run as the site.
func Serve(w http.ResponseWriter, f File, content []byte) {
	w.Header().Set("Content-Type", f.Mime)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", ContentDisposition(f.Name))
	w.Header().Set("Content-Security-Policy", "sandbox")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("Content-Length", fmt.Sprint(len(content)))
	_, _ = w.Write(content)
}

// ContentDisposition is an attachment header for name: an ASCII
// fallback and the full name in RFC 5987 form.
func ContentDisposition(name string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii, pathEscape(name))
}

// pathEscape percent-encodes everything outside RFC 5987's attr-char.
func pathEscape(s string) string {
	const ok = "!#$&+-.^_`|~"
	var b strings.Builder
	for _, c := range []byte(s) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte(ok, c) >= 0 {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

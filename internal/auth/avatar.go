package auth

import (
	"context"
	"database/sql"
	"strconv"
	"time"
)

// AvatarURL is where a person's picture is served, versioned by when it
// was set so a new one shows at once rather than after a cache expires.
func AvatarURL(userID int64, at time.Time) string {
	return "/api/admin/users/" + strconv.FormatInt(userID, 10) + "/avatar?v=" + strconv.FormatInt(at.Unix(), 10)
}

// AvatarURLs maps each of ids that has a picture to its address.
func AvatarURLs(ctx context.Context, db *sql.DB, ids []int64) map[int64]string {
	out := map[int64]string{}
	if len(ids) == 0 {
		return out
	}
	list := make([]string, len(ids))
	for i, id := range ids {
		list[i] = strconv.FormatInt(id, 10)
	}
	rows, err := db.QueryContext(ctx, `
		SELECT id, avatar_updated_at FROM users
		WHERE id = ANY(string_to_array($1, ',')::bigint[]) AND avatar_updated_at IS NOT NULL
	`, joinComma(list))
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id int64
			at time.Time
		)
		if rows.Scan(&id, &at) == nil {
			out[id] = AvatarURL(id, at)
		}
	}
	return out
}

func joinComma(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += ","
		}
		out += x
	}
	return out
}

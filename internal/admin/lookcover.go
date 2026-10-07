package admin

// lookCoverSQL selects, from the adventures row aliased alias, the look's
// starter theme (adventures.trail_theme) and its own cover picture (a
// media id of the adventure's, 0 for none): where an adventure has no
// icon, the console shows the cover the players see. NULL-safe for a
// LEFT JOIN.
func lookCoverSQL(alias string) string {
	return `COALESCE(` + alias + `.trail_theme, ''),
	        COALESCE((SELECT m.id FROM adventure_media m
	                  WHERE m.adventure_id = ` + alias + `.id AND m.kind = 'image'
	                    AND m.id = NULLIF(` + alias + `.look_style->>'cover', '')::bigint), 0)`
}

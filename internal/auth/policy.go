package auth

import (
	_ "embed"
	"errors"
	"strings"
	"sync"
)

// MinPasswordLen is the minimum length of any new password. 8 is the
// OWASP-floor for general-purpose accounts. Existing weaker passwords
// keep working — this only affects new SetPassword / ResetPassword
// calls.
const MinPasswordLen = 8

// weakPasswordsRaw is the SecLists Pwdb top-1000 list, embedded so the
// policy travels with the binary. Lines are lowercased + deduped at
// process start; lookup is exact-match against the user-supplied
// password (case-insensitive).
//
//go:embed weakpasswords.txt
var weakPasswordsRaw string

var (
	weakOnce sync.Once
	weakSet  map[string]struct{}
)

func loadWeakSet() {
	weakSet = make(map[string]struct{}, 1024)
	for _, line := range strings.Split(weakPasswordsRaw, "\n") {
		s := strings.TrimSpace(line)
		if s == "" {
			continue
		}
		weakSet[strings.ToLower(s)] = struct{}{}
	}
}

// ErrPasswordTooShort and ErrPasswordTooCommon are the two policy
// failures the API surfaces. Login/SetPassword handlers translate
// these into a 400 with a human message.
var (
	ErrPasswordTooShort  = errors.New("password is too short")
	ErrPasswordTooCommon = errors.New("password is too common; pick something else")
)

// ValidatePassword checks length + common-list membership. Returns
// nil if the password meets policy.
func ValidatePassword(pw string) error {
	if len(pw) < MinPasswordLen {
		return ErrPasswordTooShort
	}
	weakOnce.Do(loadWeakSet)
	if _, weak := weakSet[strings.ToLower(pw)]; weak {
		return ErrPasswordTooCommon
	}
	return nil
}

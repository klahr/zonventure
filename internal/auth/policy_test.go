package auth

import (
	"errors"
	"testing"
)

func TestValidatePassword(t *testing.T) {
	cases := []struct {
		name string
		pw   string
		want error
	}{
		{"empty", "", ErrPasswordTooShort},
		{"7 chars", "abcdefg", ErrPasswordTooShort},
		{"top-1k exact", "password", ErrPasswordTooCommon},
		{"top-1k case insensitive", "Password", ErrPasswordTooCommon},
		{"top-1k numeric", "12345678", ErrPasswordTooCommon},
		{"acceptable", "correct horse battery staple", nil},
		{"acceptable mixed", "wEll-pIcked-9!", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ValidatePassword(c.pw)
			if !errors.Is(got, c.want) {
				t.Fatalf("ValidatePassword(%q) = %v, want %v", c.pw, got, c.want)
			}
		})
	}
}

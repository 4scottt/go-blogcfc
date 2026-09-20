package render

import (
	"crypto/md5"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"
)

// Gravatar is the avatar URL for a commenter's address (PLAN §11,
// "Gravatar"): the md5 of the trimmed, lowercased address, over https,
// at the given size, rated pg, with defaultURL as the fallback image.
// BlogCFC used http and forgot the lowercasing in admin/moderate.cfm,
// which gave the same person two different avatars; both are fixed here
// (PLAN §7, "Bugs fixed rather than ported").
//
// The address is never shown, only hashed, and defaultURL is escaped
// into the query string.
func Gravatar(email string, size int, defaultURL string) string {
	sum := md5.Sum([]byte(strings.ToLower(strings.TrimSpace(email))))
	q := "s=" + strconv.Itoa(size) + "&r=pg"
	if defaultURL != "" {
		q += "&d=" + url.QueryEscape(defaultURL)
	}
	return "https://www.gravatar.com/avatar/" + hex.EncodeToString(sum[:]) + "?" + q
}

// Package antispam is the comment form's defence (PLAN §7, §11
// "Antispam"): the honeypot, the signed timestamp, the URL count and the
// word list that replace cfFormProtect, plus the arithmetic challenge
// that replaces the Lyla image captcha.
//
// Everything here is pure: no database, no network, no third party. The
// points and the failure limit are cfFormProtect's own ini defaults
// (client/cfformprotect/cffp.ini.cfm in the as-is), so one weak signal
// does not block a comment while the honeypot alone does.
package antispam

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"math/big"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The points each failed test costs and the limit that flags a
// submission as spam, from cfFormProtect's ini:
//
//	hiddenFieldPoints=3  timedFormPoints=2  tooManyUrlsPoints=3
//	spamStringPoints=2   failureLimit=3
//
// So the honeypot or the URL count blocks on its own, and timing or a
// word hit alone does not - but any two of them do.
const (
	HoneypotPoints = 3
	TimingPoints   = 2
	URLPoints      = 3
	WordPoints     = 2

	DefaultLimit = 3
)

// The rest of cfFormProtect's ini defaults, and the as-is field names
// (the bots of 2008 knew them; keeping them costs nothing and keeps the
// form recognisably BlogCFC's).
const (
	DefaultMinSeconds = 5
	DefaultMaxSeconds = 3600
	DefaultMaxURLs    = 6

	HoneypotField = "formfield1234567894"
	StampField    = "formfield1234567893"
)

// ChallengeTTL is how long an arithmetic challenge stays answerable.
const ChallengeTTL = time.Hour

// Check is one configured set of tests. Words is the `trackbackspamlist`
// setting and IPBlocks the `ipblocklist` setting (PLAN §10); Secret signs
// the timestamp field and the challenge token.
type Check struct {
	Honeypot               string // hidden field name; must come back empty
	MinSeconds, MaxSeconds int    // acceptable age of the signed timestamp
	MaxURLs                int    // more than this many URLs in the texts fails
	Words                  []string
	IPBlocks               []string
	Secret                 []byte
	Limit                  int // failure points that block; cfFormProtect's 3
}

// Defaults returns a Check with cfFormProtect's ini values filled in.
// The caller supplies Secret, Words and IPBlocks.
func Defaults() Check {
	return Check{
		Honeypot:   HoneypotField,
		MinSeconds: DefaultMinSeconds,
		MaxSeconds: DefaultMaxSeconds,
		MaxURLs:    DefaultMaxURLs,
		Limit:      DefaultLimit,
	}
}

// Result is what Verify found. Points is the cfFormProtect score;
// Reasons names every test that failed, for the log (never for the
// visitor, who is told only that the comment looked like spam).
type Result struct {
	Blocked bool
	Points  int
	Reasons []string
}

// Reason strings, stable so tests and logs can match on them.
const (
	ReasonHoneypot     = "honeypot"
	ReasonStampMissing = "stamp missing"
	ReasonStampInvalid = "stamp invalid"
	ReasonTooFast      = "too fast"
	ReasonTooSlow      = "too slow"
	ReasonTooManyURLs  = "too many urls"
	ReasonSpamWord     = "spam word"
	ReasonBlockedIP    = "blocked ip"
)

// Fields returns the two hidden fields the form must carry: the honeypot
// (empty, hidden, labelled "leave this empty" for a screen reader) and
// the signed timestamp. The stamp is `<unix>.<hmac>`, so a bot cannot
// forge a plausible fill time and a replayed form expires.
func (c Check) Fields(now time.Time) (honeypotName, stampField, stampValue string) {
	name := c.Honeypot
	if name == "" {
		name = HoneypotField
	}
	unix := strconv.FormatInt(now.Unix(), 10)
	return name, StampField, unix + "." + c.sign(unix)
}

// Verify runs every test against a submitted form. texts are the free
// text fields to search for spam words and URLs - for a comment the
// as-is searched the comment, the name, the website and the email, in
// that order (blog.cfc addComment).
//
// The word list and the IP block list block outright, whatever the
// score: BlogCFC refused those in addComment itself, independently of
// cfFormProtect. They still score their points so the log shows the
// whole picture.
func (c Check) Verify(form url.Values, now time.Time, remoteIP string, texts ...string) Result {
	var r Result
	always := false

	name := c.Honeypot
	if name == "" {
		name = HoneypotField
	}
	if strings.TrimSpace(form.Get(name)) != "" {
		r.Points += HoneypotPoints
		r.Reasons = append(r.Reasons, ReasonHoneypot)
	}

	if reason := c.checkStamp(form.Get(StampField), now); reason != "" {
		r.Points += TimingPoints
		r.Reasons = append(r.Reasons, reason)
	}

	maxURLs := c.MaxURLs
	if maxURLs <= 0 {
		maxURLs = DefaultMaxURLs
	}
	if CountURLs(texts...) > maxURLs {
		r.Points += URLPoints
		r.Reasons = append(r.Reasons, ReasonTooManyURLs)
	}

	if MatchWord(c.Words, texts...) != "" {
		r.Points += WordPoints
		r.Reasons = append(r.Reasons, ReasonSpamWord)
		always = true
	}

	if MatchIP(c.IPBlocks, remoteIP) {
		r.Reasons = append(r.Reasons, ReasonBlockedIP)
		always = true
	}

	limit := c.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}
	r.Blocked = always || r.Points >= limit
	return r
}

// checkStamp returns "" when the signed timestamp is present, genuine
// and of an acceptable age, else the reason it failed.
func (c Check) checkStamp(value string, now time.Time) string {
	if value == "" {
		return ReasonStampMissing
	}
	unix, mac, ok := strings.Cut(value, ".")
	if !ok {
		return ReasonStampInvalid
	}
	if subtle.ConstantTimeCompare([]byte(mac), []byte(c.sign(unix))) != 1 {
		return ReasonStampInvalid
	}
	sec, err := strconv.ParseInt(unix, 10, 64)
	if err != nil {
		return ReasonStampInvalid
	}
	lo, hi := c.MinSeconds, c.MaxSeconds
	if lo <= 0 {
		lo = DefaultMinSeconds
	}
	if hi <= 0 {
		hi = DefaultMaxSeconds
	}
	age := int(now.Unix() - sec)
	switch {
	case age < lo:
		return ReasonTooFast
	case age > hi:
		return ReasonTooSlow
	}
	return ""
}

func (c Check) sign(payload string) string {
	m := hmac.New(sha256.New, c.Secret)
	m.Write([]byte(payload))
	return hex.EncodeToString(m.Sum(nil))
}

// CountURLs counts the URLs across the given texts. cfFormProtect looked
// for `http://` only and its count was off by one (it started at -1);
// https counts here too and the count is the plain one, so "more than
// MaxURLs fails" means what it says (PLAN §11).
func CountURLs(texts ...string) int {
	n := 0
	for _, t := range texts {
		l := strings.ToLower(t)
		n += strings.Count(l, "http://") + strings.Count(l, "https://")
	}
	return n
}

// MatchWord returns the first word of the list found in any of the texts,
// case-insensitively, or "". This is addComment's `trackbackspamlist`
// loop: a substring match, not a word match, exactly as the as-is.
func MatchWord(words []string, texts ...string) string {
	for _, w := range words {
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		lw := strings.ToLower(w)
		for _, t := range texts {
			if strings.Contains(strings.ToLower(t), lw) {
				return w
			}
		}
	}
	return ""
}

// MatchIP reports whether the address is in the block list. A pattern may
// end in, or contain, `*` (addComment's `ipblocklist`: `192.168.*`); every
// other character is literal.
func MatchIP(blocks []string, ip string) bool {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return false
	}
	for _, b := range blocks {
		b = strings.TrimSpace(b)
		if b == "" {
			continue
		}
		if strings.Contains(b, "*") {
			if globMatch(b, ip) {
				return true
			}
			continue
		}
		if strings.EqualFold(b, ip) {
			return true
		}
	}
	return false
}

// globMatch matches a pattern whose only metacharacter is `*`.
func globMatch(pattern, s string) bool {
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == s
	}
	if !strings.HasPrefix(s, parts[0]) {
		return false
	}
	s = s[len(parts[0]):]
	last := parts[len(parts)-1]
	for _, p := range parts[1 : len(parts)-1] {
		i := strings.Index(s, p)
		if i < 0 {
			return false
		}
		s = s[i+len(p):]
	}
	if last == "" {
		return true
	}
	return strings.HasSuffix(s, last) && len(s) >= len(last)
}

// Challenge is the `usecaptcha` replacement (PLAN §7): a question a
// person answers and a token that carries no answer, only a signature
// over it. The form shows the question and posts the token back with the
// answer; nothing is stored server side and no image is generated.
func Challenge(secret []byte, now time.Time) (question, token string) {
	a := randInt(9) + 1
	b := randInt(9) + 1
	question = "What is " + strconv.Itoa(a) + " + " + strconv.Itoa(b) + "?"
	return question, challengeToken(secret, now.Unix(), strconv.Itoa(a+b))
}

// VerifyChallenge reports whether answer is the one Challenge asked for
// and the token was issued within ChallengeTTL of now.
func VerifyChallenge(secret []byte, token, answer string, now time.Time) bool {
	issued, mac, ok := strings.Cut(token, ".")
	if !ok || mac == "" {
		return false
	}
	sec, err := strconv.ParseInt(issued, 10, 64)
	if err != nil {
		return false
	}
	age := now.Unix() - sec
	if age < 0 || age > int64(ChallengeTTL/time.Second) {
		return false
	}
	want := challengeToken(secret, sec, normalizeAnswer(answer))
	return subtle.ConstantTimeCompare([]byte(token), []byte(want)) == 1
}

func challengeToken(secret []byte, issued int64, answer string) string {
	payload := strconv.FormatInt(issued, 10) + "|" + answer
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(payload))
	return strconv.FormatInt(issued, 10) + "." + hex.EncodeToString(m.Sum(nil))
}

// normalizeAnswer keeps only the digits (and a leading minus), so "  7 "
// and "7." both answer 7 while "seven" does not.
func normalizeAnswer(answer string) string {
	var b strings.Builder
	for i, r := range strings.TrimSpace(answer) {
		if r >= '0' && r <= '9' || (i == 0 && r == '-') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// randInt returns a uniform int in [0,n). A failing crypto/rand is not
// something a blog can do anything about; the challenge falls back to a
// fixed operand rather than panicking mid-request.
func randInt(n int) int {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0
	}
	return int(v.Int64())
}

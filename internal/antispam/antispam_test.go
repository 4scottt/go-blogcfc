package antispam

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

var secret = []byte("test-secret-not-a-real-one")

// clean returns a form that passes every test: empty honeypot, a stamp
// signed 30 s ago (inside 5..3600).
func clean(c Check, now time.Time) url.Values {
	_, stampField, stampValue := c.Fields(now.Add(-30 * time.Second))
	return url.Values{
		c.Honeypot: {""},
		stampField: {stampValue},
	}
}

func testCheck() Check {
	c := Defaults()
	c.Secret = secret
	c.Words = []string{"viagra", "cheap soma", "weight loss"}
	c.IPBlocks = []string{"10.0.0.7", "192.168.*", "*.42"}
	return c
}

// TestFP_C04_SpamWordListAndIPBlockListWithWildcards covers C04: the
// `trackbackspamlist` blocks a comment whose comment, name, website or
// email contains a term, case-insensitively, and `ipblocklist` blocks an
// address, literally or with a `*` wildcard. Both refuse outright, as
// addComment did, whatever the cfFormProtect score.
func TestFP_C04_SpamWordListAndIPBlockListWithWildcards(t *testing.T) {
	c := testCheck()
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

	t.Run("word in any field", func(t *testing.T) {
		fields := map[string][]string{
			"comment": {"I have VIAGRA for you", "Nice post", "", ""},
			"name":    {"Nice post", "Cheap Soma Dealer", "", ""},
			"website": {"Nice post", "", "Weight Loss deals", ""},
			"email":   {"Nice post", "", "", "viagra@example.com"},
		}
		for where, texts := range fields {
			got := c.Verify(clean(c, now), now, "203.0.113.9", texts...)
			if !got.Blocked {
				t.Errorf("%s: a spam word must block outright, got %+v", where, got)
			}
			if !hasReason(got, ReasonSpamWord) {
				t.Errorf("%s: reasons %v do not name the word list", where, got.Reasons)
			}
			if got.Points != WordPoints {
				t.Errorf("%s: points = %d, want %d (spamStringPoints)", where, got.Points, WordPoints)
			}
		}
	})

	t.Run("clean text passes", func(t *testing.T) {
		got := c.Verify(clean(c, now), now, "203.0.113.9", "A real comment", "Ada", "http://ada.example", "ada@example.com")
		if got.Blocked || got.Points != 0 {
			t.Fatalf("a clean comment must pass, got %+v", got)
		}
	})

	t.Run("word list is a substring match, as the as-is", func(t *testing.T) {
		if MatchWord([]string{"soma"}, "personal somatic notes") != "soma" {
			t.Error("addComment used findNoCase, a substring match")
		}
	})

	t.Run("ip block list", func(t *testing.T) {
		cases := []struct {
			ip   string
			want bool
		}{
			{"10.0.0.7", true},      // literal
			{"10.0.0.70", false},    // literal, not a prefix
			{"192.168.1.5", true},   // trailing wildcard
			{"192.168.0.0", true},   //
			{"192.1.1.1", false},    //
			{"198.51.100.42", true}, // leading wildcard
			{"198.51.100.43", false},
			{"203.0.113.9", false},
			{"", false},
		}
		for _, tc := range cases {
			if got := MatchIP(c.IPBlocks, tc.ip); got != tc.want {
				t.Errorf("MatchIP(%q) = %v, want %v", tc.ip, got, tc.want)
			}
			res := c.Verify(clean(c, now), now, tc.ip, "A real comment", "Ada", "", "ada@example.com")
			if res.Blocked != tc.want {
				t.Errorf("Verify from %q blocked = %v, want %v (%v)", tc.ip, res.Blocked, tc.want, res.Reasons)
			}
			if tc.want && !hasReason(res, ReasonBlockedIP) {
				t.Errorf("Verify from %q: reasons %v do not name the IP list", tc.ip, res.Reasons)
			}
		}
	})

	t.Run("a blocked IP blocks with no points", func(t *testing.T) {
		got := c.Verify(clean(c, now), now, "192.168.1.5", "A real comment")
		if !got.Blocked {
			t.Fatal("blocked IP must refuse outright")
		}
		if got.Points != 0 {
			t.Errorf("the IP list was never a cfFormProtect test; points = %d, want 0", got.Points)
		}
	})
}

// TestFP_C05_HoneypotTimingURLCountChallengePoints covers C05: each
// signal scores cfFormProtect's ini points and only the ones worth the
// failure limit (3) block on their own; two weak signals together do.
// The arithmetic challenge that replaces the image captcha is here too.
func TestFP_C05_HoneypotTimingURLCountChallengePoints(t *testing.T) {
	c := testCheck()
	now := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

	t.Run("points and limit are cfFormProtect's ini", func(t *testing.T) {
		if HoneypotPoints != 3 || TimingPoints != 2 || URLPoints != 3 || WordPoints != 2 || DefaultLimit != 3 {
			t.Fatalf("points changed: honeypot %d timing %d urls %d words %d limit %d",
				HoneypotPoints, TimingPoints, URLPoints, WordPoints, DefaultLimit)
		}
		if DefaultMinSeconds != 5 || DefaultMaxSeconds != 3600 || DefaultMaxURLs != 6 {
			t.Fatalf("thresholds changed: %d %d %d", DefaultMinSeconds, DefaultMaxSeconds, DefaultMaxURLs)
		}
	})

	t.Run("honeypot alone blocks", func(t *testing.T) {
		f := clean(c, now)
		f.Set(c.Honeypot, "http://spam.example")
		got := c.Verify(f, now, "203.0.113.9", "hello")
		if !got.Blocked || got.Points != HoneypotPoints || !hasReason(got, ReasonHoneypot) {
			t.Fatalf("honeypot (3 points) must reach the limit alone, got %+v", got)
		}
	})

	t.Run("too many URLs alone blocks", func(t *testing.T) {
		text := strings.Repeat("see http://x.example ", 7) // 7 > MaxURLs 6
		got := c.Verify(clean(c, now), now, "203.0.113.9", text)
		if !got.Blocked || got.Points != URLPoints || !hasReason(got, ReasonTooManyURLs) {
			t.Fatalf("7 URLs (3 points) must block alone, got %+v", got)
		}
		ok := c.Verify(clean(c, now), now, "203.0.113.9", strings.Repeat("see https://x.example ", 6))
		if ok.Blocked || ok.Points != 0 {
			t.Fatalf("6 URLs is the allowance, got %+v", ok)
		}
	})

	t.Run("timing alone does not block", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			age    time.Duration
			reason string
		}{
			{"too fast", 2 * time.Second, ReasonTooFast},
			{"too slow", 2 * time.Hour, ReasonTooSlow},
		} {
			_, field, value := c.Fields(now.Add(-tc.age))
			f := url.Values{c.Honeypot: {""}, field: {value}}
			got := c.Verify(f, now, "203.0.113.9", "hello")
			if got.Points != TimingPoints || !hasReason(got, tc.reason) {
				t.Fatalf("%s: got %+v", tc.name, got)
			}
			if got.Blocked {
				t.Fatalf("%s: 2 points is under the limit of 3, got %+v", tc.name, got)
			}
		}
	})

	t.Run("a missing or forged stamp scores timing points", func(t *testing.T) {
		for name, f := range map[string]url.Values{
			"missing":  {c.Honeypot: {""}},
			"garbage":  {c.Honeypot: {""}, StampField: {"not-a-stamp"}},
			"unsigned": {c.Honeypot: {""}, StampField: {strconv.FormatInt(now.Unix()-30, 10) + ".deadbeef"}},
		} {
			got := c.Verify(f, now, "203.0.113.9", "hello")
			if got.Points != TimingPoints || got.Blocked {
				t.Errorf("%s stamp: got %+v", name, got)
			}
		}
		// A stamp signed with another key is not this blog's.
		other := c
		other.Secret = []byte("someone else")
		_, field, value := other.Fields(now.Add(-30 * time.Second))
		got := c.Verify(url.Values{c.Honeypot: {""}, field: {value}}, now, "203.0.113.9", "hello")
		if !hasReason(got, ReasonStampInvalid) {
			t.Errorf("a stamp signed with another key must not verify: %+v", got)
		}
	})

	t.Run("two weak signals together block", func(t *testing.T) {
		// A word hit or a blocked IP would refuse outright, so the pair
		// that proves the score is timing plus the URL count.
		_, field, value := c.Fields(now.Add(-time.Second)) // too fast: 2
		f := url.Values{c.Honeypot: {""}, field: {value}}
		got := c.Verify(f, now, "203.0.113.9", strings.Repeat("http://x.example ", 7)) // urls: 3
		if got.Points != TimingPoints+URLPoints || !got.Blocked {
			t.Fatalf("timing + urls = 5 must block, got %+v", got)
		}
	})

	t.Run("challenge right, wrong and expired", func(t *testing.T) {
		question, token := Challenge(secret, now)
		answer := solve(t, question)

		if !VerifyChallenge(secret, token, answer, now.Add(30*time.Second)) {
			t.Fatalf("%q answered %q must verify", question, answer)
		}
		if !VerifyChallenge(secret, token, " "+answer+" ", now) {
			t.Error("surrounding space must not fail an answer")
		}
		wrong := strconv.Itoa(mustAtoi(t, answer) + 1)
		if VerifyChallenge(secret, token, wrong, now) {
			t.Errorf("%q answered %q must not verify", question, wrong)
		}
		if VerifyChallenge(secret, token, answer, now.Add(ChallengeTTL+time.Second)) {
			t.Error("a challenge older than an hour must not verify")
		}
		if VerifyChallenge([]byte("another blog"), token, answer, now) {
			t.Error("a token from another blog's key must not verify")
		}
		for _, bad := range []string{"", "nonsense", ".", "abc.def"} {
			if VerifyChallenge(secret, bad, answer, now) {
				t.Errorf("token %q must not verify", bad)
			}
		}
		// The answer is not in the token: a bot reading the form learns
		// nothing it could not work out by doing the sum.
		if strings.Contains(token, "|"+answer) {
			t.Error("the token must not carry the answer")
		}
	})

	t.Run("the form carries both fields", func(t *testing.T) {
		honeypot, field, value := c.Fields(now)
		if honeypot != HoneypotField || field != StampField {
			t.Fatalf("fields = %q, %q", honeypot, field)
		}
		unix, mac, ok := strings.Cut(value, ".")
		if !ok || mac == "" {
			t.Fatalf("stamp %q is not <unix>.<hmac>", value)
		}
		if unix != strconv.FormatInt(now.Unix(), 10) {
			t.Fatalf("stamp carries %q, want %d", unix, now.Unix())
		}
	})
}

func hasReason(r Result, want string) bool {
	for _, got := range r.Reasons {
		if got == want {
			return true
		}
	}
	return false
}

// solve reads the two operands out of "What is 3 + 4?".
func solve(t *testing.T, question string) string {
	t.Helper()
	var a, b int
	if _, err := fmt.Sscanf(question, "What is %d + %d?", &a, &b); err != nil {
		t.Fatalf("question %q: %v", question, err)
	}
	return strconv.Itoa(a + b)
}

func mustAtoi(t *testing.T, s string) int {
	t.Helper()
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

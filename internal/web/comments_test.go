package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/4scottt/go-blogcfc/internal/antispam"
	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/mail"
	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

// The comment pipeline's tests (PLAN §9 C01-C11). They go through HTTP
// against a real database and a recording sender, and every fixture is
// made through the store, never with raw SQL.

// mutableIdentity is an Identity a test can log in and out again, which
// C05 needs in one run.
type mutableIdentity struct{ user *store.User }

func (i *mutableIdentity) Current(*http.Request) *store.User { return i.user }

// commentSite is one wired-up public site with a sender and an identity.
type commentSite struct {
	t        *testing.T
	store    *store.Store
	settings *config.Settings
	mail     *mail.Recorder
	identity *mutableIdentity
	handler  http.Handler
}

func newCommentSite(t *testing.T) *commentSite {
	t.Helper()
	st := testdb.New(t)
	cfg := &config.Config{BlogBaseURL: testBase, Port: 8080, SessionSecret: "comment-test-secret"}
	settings := config.NewSettings(st, cfg)
	if err := settings.Reload(context.Background()); err != nil {
		t.Fatalf("settings reload: %v", err)
	}
	id := &mutableIdentity{}
	recorder := &mail.Recorder{}
	m := New(cfg, st, settings, id)
	m.Mail = recorder
	mux := http.NewServeMux()
	m.Routes(mux)
	return &commentSite{t: t, store: st, settings: settings, mail: recorder, identity: id, handler: mux}
}

func (s *commentSite) setSetting(key, value string) {
	s.t.Helper()
	if err := s.settings.Set(context.Background(), map[string]string{key: value}); err != nil {
		s.t.Fatalf("set %s: %v", key, err)
	}
}

// entry inserts one live entry that takes comments.
func (s *commentSite) entry(title, alias string) store.Entry {
	s.t.Helper()
	e := store.Entry{
		Title: title, Alias: alias, Body: "<p>" + title + "</p>",
		Posted: utc(2026, 6, 1, 9, 0), Username: "ray", Released: true, AllowComments: true,
	}
	if err := s.store.CreateEntry(context.Background(), &e); err != nil {
		s.t.Fatalf("create entry: %v", err)
	}
	return e
}

func (s *commentSite) comment(c store.Comment) store.Comment {
	s.t.Helper()
	if err := s.store.CreateComment(context.Background(), &c); err != nil {
		s.t.Fatalf("create comment: %v", err)
	}
	return c
}

func (s *commentSite) comments(entryID string, includeUnmoderated bool) []store.Comment {
	s.t.Helper()
	list, err := s.store.ListComments(context.Background(), entryID, includeUnmoderated)
	if err != nil {
		s.t.Fatalf("list comments: %v", err)
	}
	return list
}

func (s *commentSite) subscribers(entryID string) map[string]string {
	s.t.Helper()
	subs, err := s.store.ThreadSubscribers(context.Background(), entryID)
	if err != nil {
		s.t.Fatalf("thread subscribers: %v", err)
	}
	return subs
}

func (s *commentSite) get(path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	s.t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, r)
	return rec
}

func (s *commentSite) getOK(path string, cookies ...*http.Cookie) string {
	s.t.Helper()
	rec := s.get(path, cookies...)
	if rec.Code != http.StatusOK {
		s.t.Fatalf("GET %s = %d, want 200", path, rec.Code)
	}
	return rec.Body.String()
}

func (s *commentSite) post(path string, form url.Values) *httptest.ResponseRecorder {
	s.t.Helper()
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, r)
	return rec
}

// The antispam fields a rendered form carries, read back out of it the
// way a browser would fill them in.
var (
	stampValueRe     = regexp.MustCompile(`name="` + antispam.StampField + `" value="([^"]*)"`)
	challengeTokenRe = regexp.MustCompile(`name="` + challengeTokenField + `" value="([^"]*)"`)
	// html/template writes a `+` in a text node as `&#43;`, so the
	// question reads "What is 2 &#43; 8?" on the wire.
	challengeRe = regexp.MustCompile(`<span id="challenge">What is (\d+) (?:\+|&#43;) (\d+)\?</span>`)
)

// fillAntispam returns the hidden fields and the challenge answer a form
// just fetched expects back, so a test posts what a person would.
func fillAntispam(t *testing.T, form string) url.Values {
	t.Helper()
	v := url.Values{}
	if m := stampValueRe.FindStringSubmatch(form); m != nil {
		v.Set(antispam.StampField, m[1])
	} else {
		t.Fatalf("the form carries no signed timestamp:\n%s", form)
	}
	if m := challengeRe.FindStringSubmatch(form); m != nil {
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		v.Set(challengeAnswerField, strconv.Itoa(a+b))
		tok := challengeTokenRe.FindStringSubmatch(form)
		if tok == nil {
			t.Fatalf("the form asks a question and carries no token:\n%s", form)
		}
		v.Set(challengeTokenField, tok[1])
	}
	return v
}

// commentForm fetches the add-comment form and returns a filled-in POST
// body: the antispam fields from the page, plus whatever the caller
// wants on top.
func (s *commentSite) commentForm(entryID string, fields url.Values) url.Values {
	s.t.Helper()
	form := s.getOK("/comments/add/" + entryID)
	v := fillAntispam(s.t, form)
	v.Set("addcomment", postCommentLabel)
	for k, vals := range fields {
		v[k] = vals
	}
	return v
}

// TestFP_C01_AddCommentValidation is C01: the form's fields, the four
// messages addcomment.cfm could print, and the `http://` placeholder,
// which is stripped rather than stored or refused.
func TestFP_C01_AddCommentValidation(t *testing.T) {
	s := newCommentSite(t)
	e := s.entry("Talk to me", "talk-to-me")

	form := s.getOK("/comments/add/" + e.ID)
	for _, want := range []string{
		`<body id="popUpFormBody">`,
		`<form action="` + testBase + `/comments/add/` + e.ID + `" method="post"`,
		`<input type="text" id="name" name="name"`,
		`<input type="text" id="email" name="email"`,
		`<input type="text" id="website" name="website" value="http://"`,
		`<textarea id="comment" name="comment"`,
		`id="subscribe" name="subscribe"`,
		`id="remember" name="remember"`,
		`name="` + antispam.HoneypotField + `"`,
		`name="` + antispam.StampField + `"`,
		`name="` + challengeTokenField + `"`,
		`name="` + challengeAnswerField + `"`,
		`value="` + postCommentLabel + `" />`,
	} {
		if !strings.Contains(form, want) {
			t.Errorf("the comment form is missing %q", want)
		}
	}

	// Nothing filled in: three of the four messages (the website box is
	// empty, so it has nothing to complain about).
	rec := s.post("/comments/add/"+e.ID, s.commentForm(e.ID, nil))
	body := rec.Body.String()
	for _, want := range []string{
		"Please correct the following issue(s)",
		"You must include your name.",
		"You must include a valid email address.",
		"You must include your comments.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the empty form is missing %q", want)
		}
	}
	if n := len(s.comments(e.ID, true)); n != 0 {
		t.Fatalf("an invalid form stored %d comments", n)
	}

	// A website that is not a URL is refused; a good one is not.
	rec = s.post("/comments/add/"+e.ID, s.commentForm(e.ID, url.Values{
		"name": {"Ray"}, "email": {"ray@camdenfamily.com"},
		"website": {"not a url"}, "comment": {"Hello."},
	}))
	if !strings.Contains(rec.Body.String(), "Your website was not a valid url.") {
		t.Errorf("a bad website was accepted:\n%s", rec.Body.String())
	}

	// The placeholder is stripped, not stored and not refused.
	s.post("/comments/add/"+e.ID, s.commentForm(e.ID, url.Values{
		"name": {"Ray"}, "email": {"ray@camdenfamily.com"},
		"website": {websitePlaceholder}, "comment": {"Hello."},
	}))
	list := s.comments(e.ID, true)
	if len(list) != 1 {
		t.Fatalf("a valid comment stored %d rows", len(list))
	}
	if list[0].Website != "" {
		t.Errorf("website = %q, want the placeholder stripped", list[0].Website)
	}
	if list[0].Name != "Ray" || list[0].Comment != "Hello." {
		t.Errorf("the stored comment is %+v", list[0])
	}
	if list[0].KillToken == "" {
		t.Error("the comment has no kill token")
	}
}

// TestFP_C02_SanitisingAndTruncation is C02: the as-is column widths,
// counted in characters, and markup that survives storage as text and is
// escaped where it is shown.
func TestFP_C02_SanitisingAndTruncation(t *testing.T) {
	// The unit half: left() counts characters, not bytes.
	for _, tc := range []struct {
		in   string
		max  int
		want string
	}{
		{"Ray", 50, "Ray"},
		{strings.Repeat("a", 60), 50, strings.Repeat("a", 50)},
		{strings.Repeat("é", 60), 50, strings.Repeat("é", 50)},
	} {
		if got := truncate(tc.in, tc.max); got != tc.want {
			t.Errorf("truncate(%d runes, %d) gave %d runes", len([]rune(tc.in)), tc.max, len([]rune(got)))
		}
	}

	s := newCommentSite(t)
	s.setSetting("moderate", "no")
	e := s.entry("Long fields", "long-fields")

	longName := strings.Repeat("n", 80)
	longLocal := strings.Repeat("e", 60)
	longSite := "http://example.com/" + strings.Repeat("p", 300)
	s.post("/comments/add/"+e.ID, s.commentForm(e.ID, url.Values{
		"name": {longName}, "email": {longLocal + "@example.com"},
		"website": {longSite}, "comment": {"<b>bold</b> & <script>alert(1)</script>"},
	}))
	list := s.comments(e.ID, true)
	if len(list) != 1 {
		t.Fatalf("stored %d comments, want one", len(list))
	}
	c := list[0]
	if len([]rune(c.Name)) != maxNameLength {
		t.Errorf("the name is %d characters, want %d", len([]rune(c.Name)), maxNameLength)
	}
	if len([]rune(c.Email)) != maxEmailLength {
		t.Errorf("the email is %d characters, want %d", len([]rune(c.Email)), maxEmailLength)
	}
	if len([]rune(c.Website)) != maxWebsiteLength {
		t.Errorf("the website is %d characters, want %d", len([]rune(c.Website)), maxWebsiteLength)
	}
	// The store keeps the text as typed; the page escapes it.
	if c.Comment != "<b>bold</b> & <script>alert(1)</script>" {
		t.Errorf("the stored comment was altered: %q", c.Comment)
	}
	page := s.getOK("/2026/6/1/long-fields")
	if strings.Contains(page, "<script>alert(1)</script>") {
		t.Error("a comment's markup reached the page unescaped")
	}
	if !strings.Contains(page, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Error("the comment's markup is not on the page escaped")
	}
}

// TestFP_C03_RefusedWhenDisallowedOrMissing is C03: an entry nobody has
// and an entry that has closed its comments.
func TestFP_C03_RefusedWhenDisallowedOrMissing(t *testing.T) {
	s := newCommentSite(t)
	closed := store.Entry{
		Title: "Closed", Alias: "closed-thread", Body: "<p>x</p>",
		Posted: utc(2026, 6, 2, 9, 0), Username: "ray", Released: true, AllowComments: false,
	}
	if err := s.store.CreateEntry(context.Background(), &closed); err != nil {
		t.Fatalf("create entry: %v", err)
	}

	if rec := s.get("/comments/add/99999999-9999-4999-8999-999999999999"); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown entry = %d, want 404", rec.Code)
	}
	if rec := s.get("/comments/subscribe/99999999-9999-4999-8999-999999999999"); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown entry's subscription = %d, want 404", rec.Code)
	}

	form := s.getOK("/comments/add/" + closed.ID)
	if !strings.Contains(form, "Comments are not allowed for this entry.") {
		t.Errorf("a closed entry still offers the form:\n%s", form)
	}
	if strings.Contains(form, `name="comment"`) {
		t.Error("a closed entry rendered the comment box")
	}
	sub := s.getOK("/comments/subscribe/" + closed.ID)
	if !strings.Contains(sub, "This entry has disabled comments") {
		t.Errorf("a closed entry still offers a subscription:\n%s", sub)
	}

	s.post("/comments/add/"+closed.ID, url.Values{
		"addcomment": {postCommentLabel}, "name": {"Ray"},
		"email": {"ray@camdenfamily.com"}, "comment": {"Hello."},
	})
	s.post("/comments/subscribe/"+closed.ID, url.Values{
		"addsub": {"Subscribe"}, "email": {"ray@camdenfamily.com"},
	})
	if n := len(s.comments(closed.ID, true)); n != 0 {
		t.Fatalf("a closed entry took %d comments", n)
	}
	if n := len(s.subscribers(closed.ID)); n != 0 {
		t.Fatalf("a closed entry took %d subscriptions", n)
	}
}

// TestFP_C05_AntispamSkippedWhenLoggedIn is C05: a filled honeypot blocks
// a visitor, does not block a logged-in author, and is not looked at when
// `usecfp` is off.
func TestFP_C05_AntispamSkippedWhenLoggedIn(t *testing.T) {
	s := newCommentSite(t)
	s.setSetting("moderate", "no")
	e := s.entry("Bait", "bait")

	honeypot := func() url.Values {
		return s.commentForm(e.ID, url.Values{
			"name": {"Bot"}, "email": {"bot@example.com"}, "comment": {"Buy things."},
			antispam.HoneypotField: {"filled in"},
		})
	}

	rec := s.post("/comments/add/"+e.ID, honeypot())
	if !strings.Contains(rec.Body.String(), spamRefused) {
		t.Errorf("a filled honeypot was let through:\n%s", rec.Body.String())
	}
	if n := len(s.comments(e.ID, true)); n != 0 {
		t.Fatalf("a filled honeypot stored %d comments", n)
	}

	// Logged in, the tests are not run at all (addcomment.cfm's
	// `not isLoggedIn()`).
	s.identity.user = &store.User{Username: "ray", Name: "Raymond Camden"}
	rec = s.post("/comments/add/"+e.ID, honeypot())
	if strings.Contains(rec.Body.String(), spamRefused) {
		t.Errorf("a logged-in author was called a spammer:\n%s", rec.Body.String())
	}
	if n := len(s.comments(e.ID, true)); n != 1 {
		t.Fatalf("a logged-in author's comment was not stored (%d rows)", n)
	}
	s.identity.user = nil

	// `usecfp` off: cfFormProtect's own tests are skipped for visitors too.
	s.setSetting("usecfp", "no")
	rec = s.post("/comments/add/"+e.ID, honeypot())
	if strings.Contains(rec.Body.String(), spamRefused) {
		t.Errorf("usecfp=no still ran the honeypot:\n%s", rec.Body.String())
	}
	if n := len(s.comments(e.ID, true)); n != 2 {
		t.Fatalf("usecfp=no stored %d comments, want two", n)
	}

	// The word list is not cfFormProtect's and never switches off.
	s.setSetting("trackbackspamlist", "texas-holdem")
	rec = s.post("/comments/add/"+e.ID, s.commentForm(e.ID, url.Values{
		"name": {"Bot"}, "email": {"bot@example.com"}, "comment": {"Free TEXAS-HOLDEM here."},
	}))
	if !strings.Contains(rec.Body.String(), spamRefused) {
		t.Errorf("the word list did not block with usecfp off:\n%s", rec.Body.String())
	}
}

// TestFP_C06_ModerationHidesUnmoderated is C06: `moderate=yes` stores a
// visitor's comment unmoderated and the entry page does not show it; a
// logged-in author's own comment overrides moderation.
func TestFP_C06_ModerationHidesUnmoderated(t *testing.T) {
	s := newCommentSite(t)
	e := s.entry("Held", "held")

	s.post("/comments/add/"+e.ID, s.commentForm(e.ID, url.Values{
		"name": {"Visitor"}, "email": {"visitor@example.com"}, "comment": {"Waiting."},
	}))
	all := s.comments(e.ID, true)
	if len(all) != 1 {
		t.Fatalf("stored %d comments, want one", len(all))
	}
	if all[0].Moderated {
		t.Error("moderate=yes stored a visitor's comment already moderated")
	}
	page := s.getOK("/2026/6/1/held")
	if strings.Contains(page, "Waiting.") {
		t.Error("an unmoderated comment is on the entry page")
	}
	if !strings.Contains(page, `<h3 class="commentHeader">Comments (0)</h3>`) {
		t.Error("an unmoderated comment was counted")
	}

	// The author is logged in: overridemoderation.
	s.identity.user = &store.User{Username: "ray"}
	s.post("/comments/add/"+e.ID, s.commentForm(e.ID, url.Values{
		"name": {"Ray"}, "email": {"ray@camdenfamily.com"}, "comment": {"Mine goes up."},
	}))
	s.identity.user = nil
	if list := s.comments(e.ID, false); len(list) != 1 || list[0].Name != "Ray" {
		t.Fatalf("the author's own comment was held: %+v", list)
	}

	// Moderation off: a visitor's comment is live at once.
	s.setSetting("moderate", "no")
	s.post("/comments/add/"+e.ID, s.commentForm(e.ID, url.Values{
		"name": {"Visitor"}, "email": {"visitor@example.com"}, "comment": {"Straight up."},
	}))
	if !strings.Contains(s.getOK("/2026/6/1/held"), "Straight up.") {
		t.Error("moderate=no still held a comment")
	}
}

// TestFP_C07_RememberMeCookies is C07: the three cookies, read back into
// the form, and expired again when the box is cleared.
func TestFP_C07_RememberMeCookies(t *testing.T) {
	s := newCommentSite(t)
	s.setSetting("moderate", "no")
	e := s.entry("Remember", "remember-me")

	rec := s.post("/comments/add/"+e.ID, s.commentForm(e.ID, url.Values{
		"name": {"Ray"}, "email": {"ray@camdenfamily.com"},
		"website": {"http://camdenfamily.com"}, "comment": {"First."},
		"remember": {"1"},
	}))
	set := map[string]*http.Cookie{}
	for _, c := range rec.Result().Cookies() {
		set[c.Name] = c
	}
	for name, want := range map[string]string{
		cookieName: "Ray", cookieEmail: "ray@camdenfamily.com", cookieWebsite: "http://camdenfamily.com",
	} {
		c, ok := set[name]
		if !ok {
			t.Fatalf("no %s cookie was set", name)
		}
		if c.Value != want {
			t.Errorf("%s = %q, want %q", name, c.Value, want)
		}
		if c.MaxAge <= 0 {
			t.Errorf("%s expires at once (MaxAge %d)", name, c.MaxAge)
		}
	}

	// A fresh visit to the form comes back filled in.
	form := s.getOK("/comments/add/"+e.ID, set[cookieName], set[cookieEmail], set[cookieWebsite])
	for _, want := range []string{
		`id="name" name="name" value="Ray"`,
		`id="email" name="email" value="ray@camdenfamily.com"`,
		`id="website" name="website" value="http://camdenfamily.com"`,
		`id="remember" name="remember" value="1" checked="checked"`,
	} {
		if !strings.Contains(form, want) {
			t.Errorf("the remembered form is missing %q", want)
		}
	}

	// Posting without the box clears them.
	rec = s.post("/comments/add/"+e.ID, s.commentForm(e.ID, url.Values{
		"name": {"Ray"}, "email": {"ray@camdenfamily.com"}, "comment": {"Second."},
	}))
	for _, c := range rec.Result().Cookies() {
		if c.Name == cookieName || c.Name == cookieEmail || c.Name == cookieWebsite {
			if c.MaxAge >= 0 || c.Value != "" {
				t.Errorf("%s was not cleared: %+v", c.Name, c)
			}
		}
	}
}

// TestFP_C08_SubscribeFlagRetroClears is C08: unticking the box turns off
// this address's earlier subscriptions on the entry.
func TestFP_C08_SubscribeFlagRetroClears(t *testing.T) {
	s := newCommentSite(t)
	s.setSetting("moderate", "no")
	e := s.entry("Thread", "thread")

	s.post("/comments/add/"+e.ID, s.commentForm(e.ID, url.Values{
		"name": {"Ray"}, "email": {"ray@camdenfamily.com"},
		"comment": {"Subscribe me."}, "subscribe": {"1"},
	}))
	if _, ok := s.subscribers(e.ID)["ray@camdenfamily.com"]; !ok {
		t.Fatal("the ticked box did not subscribe")
	}

	// Somebody else's subscription is not touched by Ray's second post.
	s.comment(store.Comment{
		EntryID: e.ID, Name: "Pete", Email: "pete@example.com",
		Comment: "Me too.", Posted: utc(2026, 6, 1, 10, 0), Moderated: true, Subscribe: true,
	})

	s.post("/comments/add/"+e.ID, s.commentForm(e.ID, url.Values{
		"name": {"Ray"}, "email": {"ray@camdenfamily.com"}, "comment": {"Second thoughts."},
	}))
	subs := s.subscribers(e.ID)
	if _, ok := subs["ray@camdenfamily.com"]; ok {
		t.Error("the unticked box did not clear the earlier subscription")
	}
	if _, ok := subs["pete@example.com"]; !ok {
		t.Error("somebody else's subscription was cleared as well")
	}
}

// TestFP_C09_SubscribeOnlyRow is C09: addsub.cfm's form, the row it
// makes, and the fact that the row is not a comment.
func TestFP_C09_SubscribeOnlyRow(t *testing.T) {
	s := newCommentSite(t)
	s.setSetting("moderate", "no")
	e := s.entry("Subscribable", "subscribable")

	form := s.getOK("/comments/subscribe/" + e.ID)
	for _, want := range []string{
		`<body id="popUpFormBody">`,
		`<form action="` + testBase + `/comments/subscribe/` + e.ID + `" method="post"`,
		`<input type="text" id="email" name="email"`,
		`value="Subscribe" />`,
	} {
		if !strings.Contains(form, want) {
			t.Errorf("the subscribe form is missing %q", want)
		}
	}

	// A bad address is refused and nothing is stored.
	rec := s.post("/comments/subscribe/"+e.ID, url.Values{"addsub": {"Subscribe"}, "email": {"nope"}})
	if !strings.Contains(rec.Body.String(), "You must include a valid email address.") {
		t.Errorf("a bad address was accepted:\n%s", rec.Body.String())
	}

	rec = s.post("/comments/subscribe/"+e.ID, url.Values{
		"addsub": {"Subscribe"}, "email": {"watcher@example.com"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("a good subscription = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), subscribedThanks) {
		t.Errorf("the confirmation is missing:\n%s", rec.Body.String())
	}

	rows, err := s.store.ListComments(context.Background(), e.ID, true)
	if err != nil {
		t.Fatalf("list comments: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("the subscription shows as a comment: %+v", rows)
	}
	if n, err := s.store.CountComments(context.Background(), e.ID); err != nil || n != 0 {
		t.Fatalf("the subscription was counted (%d, %v)", n, err)
	}
	if id, ok := s.subscribers(e.ID)["watcher@example.com"]; !ok || id == "" {
		t.Fatalf("the subscribe-only row is not a thread subscriber: %v", s.subscribers(e.ID))
	}
	// And the entry page is unchanged by it.
	if !strings.Contains(s.getOK("/2026/6/1/subscribable"), `<h3 class="commentHeader">Comments (0)</h3>`) {
		t.Error("the subscription changed the comment count on the page")
	}
}

// TestFP_C10_NotificationRecipientsAndLinks is C10: who gets the mail,
// what each of them gets in place of `%unsubscribe%`, who does not get it
// at all, and the moderated case where only the owner hears about it.
func TestFP_C10_NotificationRecipientsAndLinks(t *testing.T) {
	s := newCommentSite(t)
	s.setSetting("moderate", "no")
	s.setSetting("owneremail", "owner@example.com")
	s.setSetting("commentsfrom", "comments@example.com")
	e := s.entry("Noisy", "noisy")

	// One thread subscriber, one subscriber who is about to comment
	// again (and so must not be mailed their own comment).
	sub := s.comment(store.Comment{
		EntryID: e.ID, Name: "Pete", Email: "pete@example.com", Comment: "Watching.",
		Posted: utc(2026, 6, 1, 10, 0), Moderated: true, Subscribe: true,
	})
	s.comment(store.Comment{
		EntryID: e.ID, Name: "Ray", Email: "ray@camdenfamily.com", Comment: "Also watching.",
		Posted: utc(2026, 6, 1, 10, 30), Moderated: true, Subscribe: true,
	})

	s.mail.Reset()
	s.post("/comments/add/"+e.ID, s.commentForm(e.ID, url.Values{
		"name": {"Ray"}, "email": {"ray@camdenfamily.com"},
		"comment": {"One more."}, "subscribe": {"1"},
	}))

	msgs := s.mail.Messages()
	if len(msgs) != 2 {
		t.Fatalf("sent %d messages, want the subscriber and the owner: %+v", len(msgs), msgs)
	}
	byTo := map[string]mail.Message{}
	for _, m := range msgs {
		if len(m.To) != 1 {
			t.Fatalf("a message went to %v, want exactly one address", m.To)
		}
		byTo[m.To[0]] = m
		if m.From != "comments@example.com" {
			t.Errorf("From = %q, want the commentsfrom setting", m.From)
		}
		if strings.Contains(m.Body, mail.UnsubscribePlaceholder) {
			t.Errorf("the placeholder survived into %v's copy", m.To)
		}
	}
	if _, ok := byTo["ray@camdenfamily.com"]; ok {
		t.Error("the comment's own author was mailed their own comment")
	}

	peteMail, ok := byTo["pete@example.com"]
	if !ok {
		t.Fatalf("the thread subscriber was not mailed: %v", byTo)
	}
	wantUnsub := testBase + "/unsubscribe?email=" + url.QueryEscape("pete@example.com") + "&commentID=" + sub.ID
	if !strings.Contains(peteMail.Body, wantUnsub) {
		t.Errorf("the subscriber's unsubscribe link is not their own comment's:\n%s", peteMail.Body)
	}

	ownerMail, ok := byTo["owner@example.com"]
	if !ok {
		t.Fatalf("the owner was not mailed: %v", byTo)
	}
	list := s.comments(e.ID, true)
	posted := list[len(list)-1]
	if !strings.Contains(ownerMail.Body, testBase+"/?killcomment="+posted.KillToken) {
		t.Errorf("the owner's copy has no Delete link:\n%s", ownerMail.Body)
	}
	if strings.Contains(ownerMail.Body, "approvecomment=") {
		t.Error("the owner was offered Approve on a blog that does not moderate")
	}
	if strings.Contains(ownerMail.Body, "/unsubscribe?") {
		t.Error("the owner was offered an unsubscribe link")
	}

	// Moderating: only the owner hears, and their copy carries Approve.
	s.setSetting("moderate", "yes")
	s.mail.Reset()
	s.post("/comments/add/"+e.ID, s.commentForm(e.ID, url.Values{
		"name": {"Stranger"}, "email": {"stranger@example.com"}, "comment": {"Held one."},
	}))
	msgs = s.mail.Messages()
	if len(msgs) != 1 || len(msgs[0].To) != 1 || msgs[0].To[0] != "owner@example.com" {
		t.Fatalf("a held comment was mailed to %+v, want the owner alone", msgs)
	}
	// Pick the held comment by its text: several comments in this test
	// share a posting second, so "the last by posted" is not stable.
	var last store.Comment
	for _, c := range s.comments(e.ID, true) {
		if c.Comment == "Held one." {
			last = c
		}
	}
	if last.ID == "" {
		t.Fatal("the held comment was not stored")
	}
	if !strings.Contains(msgs[0].Body, testBase+"/?approvecomment="+last.ID) {
		t.Errorf("the owner's copy has no Approve link while moderating:\n%s", msgs[0].Body)
	}
	if !strings.Contains(msgs[0].Body, testBase+"/?killcomment="+last.KillToken) {
		t.Errorf("the owner's copy has no Delete link:\n%s", msgs[0].Body)
	}
}

// TestFP_C11_KillByTokenAndApproveById is C11: the owner's two one-click
// links, which take no login -- the token is the credential, and the
// as-is ran both in onRequestStart for anybody holding the URL.
func TestFP_C11_KillByTokenAndApproveById(t *testing.T) {
	s := newCommentSite(t)
	e := s.entry("One click", "one-click")
	kill := s.comment(store.Comment{
		EntryID: e.ID, Name: "Spammer", Email: "spam@example.com", Comment: "Buy things.",
		Posted: utc(2026, 6, 1, 11, 0), Moderated: true,
	})
	hold := s.comment(store.Comment{
		EntryID: e.ID, Name: "Held", Email: "held@example.com", Comment: "Please approve.",
		Posted: utc(2026, 6, 1, 12, 0), Moderated: false,
	})

	rec := s.get("/?killcomment=" + kill.KillToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("killcomment = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), commentKilled) {
		t.Errorf("the kill link said nothing:\n%s", rec.Body.String())
	}
	if _, err := s.store.GetComment(context.Background(), kill.ID); err == nil {
		t.Error("the comment is still there after its kill link was followed")
	}

	// A token nobody has says so rather than failing.
	rec = s.get("/?killcomment=not-a-token")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), commentGone) {
		t.Errorf("an unknown kill token = %d:\n%s", rec.Code, rec.Body.String())
	}

	rec = s.get("/?approvecomment=" + hold.ID)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), commentApproved) {
		t.Fatalf("approvecomment = %d:\n%s", rec.Code, rec.Body.String())
	}
	got, err := s.store.GetComment(context.Background(), hold.ID)
	if err != nil {
		t.Fatalf("get comment: %v", err)
	}
	if !got.Moderated {
		t.Error("the approve link did not approve the comment")
	}
	if !strings.Contains(s.getOK("/2026/6/1/one-click"), "Please approve.") {
		t.Error("the approved comment is not on the entry page")
	}

	// The home page still works when neither is asked for.
	if body := s.getOK("/"); strings.Contains(body, commentKilled) || strings.Contains(body, commentApproved) {
		t.Error("the plain home page shows a comment action message")
	}
}

// TestContactRefusesHoneypotFilledPost is the antispam block on the
// contact form (PLAN §11 "Antispam", the `// antispam: M3` slot): the
// same honeypot as the comment form, on a page that has no comment.
func TestContactRefusesHoneypotFilledPost(t *testing.T) {
	s := newCommentSite(t)
	s.setSetting("owneremail", "owner@example.com")

	form := s.getOK("/contact")
	for _, want := range []string{
		`name="` + antispam.HoneypotField + `"`,
		`name="` + antispam.StampField + `"`,
		`name="` + challengeAnswerField + `"`,
	} {
		if !strings.Contains(form, want) {
			t.Errorf("the contact form is missing %q", want)
		}
	}

	fields := fillAntispam(t, form)
	fields.Set("send", "Send Your Comments")
	fields.Set("name", "Bot")
	fields.Set("email", "bot@example.com")
	fields.Set("comments", "Buy things.")
	fields.Set(antispam.HoneypotField, "filled in")

	rec := s.post("/contact", fields)
	if !strings.Contains(rec.Body.String(), spamRefused) {
		t.Errorf("a filled honeypot was let through:\n%s", rec.Body.String())
	}
	if n := len(s.mail.Messages()); n != 0 {
		t.Fatalf("a honeypot-filled contact form sent %d messages", n)
	}

	// The same post without the honeypot goes through, challenge and all.
	fields.Del(antispam.HoneypotField)
	if rec := s.post("/contact", fields); rec.Code != http.StatusOK {
		t.Fatalf("a clean contact form = %d", rec.Code)
	}
	if n := len(s.mail.Messages()); n != 1 {
		t.Fatalf("a clean contact form sent %d messages, want one", n)
	}
}

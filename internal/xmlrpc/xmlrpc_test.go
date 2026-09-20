package xmlrpc

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/4scottt/go-blogcfc/internal/auth"
	"github.com/4scottt/go-blogcfc/internal/cache"
	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

// testBase is deliberately not the httptest host: every link a method
// answers with must come from BLOG_BASE_URL (PLAN §6, FP O05).
const testBase = "http://blog.example"

const testPassword = "xmlrpc-password"

type harness struct {
	t        *testing.T
	store    *store.Store
	settings *config.Settings
	module   *Module
	handler  http.Handler
	dataDir  string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	st := testdb.New(t)
	dir := t.TempDir()
	cfg := &config.Config{BlogBaseURL: testBase, Port: 8080, DataDir: dir}
	settings := config.NewSettings(st, cfg)
	if err := settings.Reload(context.Background()); err != nil {
		t.Fatalf("settings reload: %v", err)
	}
	m := New(cfg, st, settings)
	mux := http.NewServeMux()
	m.Routes(mux)
	return &harness{t: t, store: st, settings: settings, module: m, handler: mux, dataDir: dir}
}

// user creates a user with the Admin role and the test password.
func (h *harness) user(username string) *store.User {
	h.t.Helper()
	ctx := context.Background()
	roles, err := h.store.ListRoles(ctx)
	if err != nil {
		h.t.Fatalf("ListRoles: %v", err)
	}
	var ids []int
	for _, r := range roles {
		if strings.EqualFold(r.Role, auth.RoleAdmin) {
			ids = append(ids, r.ID)
		}
	}
	hash, err := auth.HashPassword(testPassword)
	if err != nil {
		h.t.Fatalf("HashPassword: %v", err)
	}
	u := &store.User{Username: username, PasswordHash: hash, Name: username}
	if err := h.store.CreateUser(ctx, u, ids); err != nil {
		h.t.Fatalf("CreateUser(%s): %v", username, err)
	}
	return u
}

func (h *harness) category(name, alias string) *store.Category {
	h.t.Helper()
	c := &store.Category{Name: name, Alias: alias}
	if err := h.store.CreateCategory(context.Background(), c); err != nil {
		h.t.Fatalf("CreateCategory(%s): %v", name, err)
	}
	return c
}

func (h *harness) entry(e *store.Entry) *store.Entry {
	h.t.Helper()
	if err := h.store.CreateEntry(context.Background(), e); err != nil {
		h.t.Fatalf("CreateEntry(%s): %v", e.Title, err)
	}
	return e
}

func (h *harness) reload(id string) *store.Entry {
	h.t.Helper()
	e, err := h.store.GetEntry(context.Background(), id)
	if err != nil {
		h.t.Fatalf("GetEntry(%s): %v", id, err)
	}
	return e
}

// post sends one call and decodes the reply.
func (h *harness) post(query, method string, params ...any) *Response {
	h.t.Helper()
	body, err := EncodeCall(method, params...)
	if err != nil {
		h.t.Fatalf("EncodeCall(%s): %v", method, err)
	}
	req := httptest.NewRequest(http.MethodPost, "/xmlrpc"+query, bytes.NewReader(body))
	req.Header.Set("Content-Type", "text/xml")
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		h.t.Fatalf("%s: status %d, want 200 (a fault is a 200)", method, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/xml") {
		h.t.Fatalf("%s: content-type %q", method, ct)
	}
	if !strings.HasPrefix(rec.Body.String(), Prolog) {
		h.t.Fatalf("%s: response does not start with the UTF-8 prolog: %.60q", method, rec.Body.String())
	}
	resp, err := DecodeResponse(rec.Body.Bytes())
	if err != nil {
		h.t.Fatalf("%s: DecodeResponse: %v\n%s", method, err, rec.Body.String())
	}
	return resp
}

// value runs a call that must succeed and returns its one value.
func (h *harness) value(method string, params ...any) any {
	h.t.Helper()
	resp := h.post("", method, params...)
	if resp.Fault != nil {
		h.t.Fatalf("%s: fault %d %q, want a value", method, resp.Fault.Code, resp.Fault.String)
	}
	if len(resp.Params) != 1 {
		h.t.Fatalf("%s: %d params, want 1", method, len(resp.Params))
	}
	return resp.Params[0]
}

// fault runs a call that must fail and returns the fault.
func (h *harness) fault(method string, params ...any) Fault {
	h.t.Helper()
	resp := h.post("", method, params...)
	if resp.Fault == nil {
		h.t.Fatalf("%s: %#v, want a fault", method, resp.Params)
	}
	return *resp.Fault
}

func (h *harness) array(method string, params ...any) Array {
	h.t.Helper()
	v := h.value(method, params...)
	arr, ok := v.(Array)
	if !ok {
		h.t.Fatalf("%s: %#v, want an array", method, v)
	}
	return arr
}

func (h *harness) structOf(method string, params ...any) Struct {
	h.t.Helper()
	v := h.value(method, params...)
	st, ok := v.(Struct)
	if !ok {
		h.t.Fatalf("%s: %#v, want a struct", method, v)
	}
	return st
}

// TestFP_X02_AuthOnEveryCallBadPasswordFaults is X02: no method does any
// work for a caller who cannot prove who they are, and the answer is a
// fault rather than the as-is's empty packet.
func TestFP_X02_AuthOnEveryCallBadPasswordFaults(t *testing.T) {
	h := newHarness(t)
	u := h.user("ray")
	e := h.entry(&store.Entry{Title: "Kept", Alias: "kept", Body: "body",
		Posted:   time.Now().UTC().Add(-time.Hour).Truncate(time.Second),
		Username: u.Username, AllowComments: true, Released: true})

	media := Struct{{Name: "name", Value: "x.png"}, {Name: "type", Value: "image/png"}, {Name: "bits", Value: []byte("bits")}}
	post := Struct{{Name: "title", Value: "No"}, {Name: "description", Value: "No"}}

	calls := []struct {
		method string
		params []any
	}{
		{"blogger.getUsersBlogs", []any{"key", u.Username, "wrong"}},
		{"metaWeblog.getUsersBlogs", []any{"key", u.Username, "wrong"}},
		{"metaWeblog.getCategories", []any{"key", u.Username, "wrong"}},
		{"mt.getCategoryList", []any{"key", u.Username, "wrong"}},
		{"metaWeblog.getRecentPosts", []any{"key", u.Username, "wrong", 10}},
		{"metaWeblog.getPost", []any{e.ID, u.Username, "wrong"}},
		{"metaWeblog.newPost", []any{"key", u.Username, "wrong", post, true}},
		{"metaWeblog.editPost", []any{e.ID, u.Username, "wrong", post, true}},
		{"blogger.deletePost", []any{"key", e.ID, u.Username, "wrong", true}},
		{"metaWeblog.newMediaObject", []any{blogID, u.Username, "wrong", media}},
		{"mt.getPostCategories", []any{e.ID, u.Username, "wrong"}},
		{"mt.setPostCategories", []any{e.ID, u.Username, "wrong", Array{}}},
	}
	if len(calls) != 12 {
		t.Fatalf("the table covers %d methods, the blog answers 12", len(calls))
	}
	for _, c := range calls {
		f := h.fault(c.method, c.params...)
		if f.Code != FaultAuth {
			t.Errorf("%s with a bad password: fault code %d, want %d", c.method, f.Code, FaultAuth)
		}
		if f.String == "" {
			t.Errorf("%s with a bad password: empty faultString", c.method)
		}
	}

	// An unknown user is the same fault, and neither the write methods
	// nor the media method left anything behind.
	if f := h.fault("metaWeblog.getRecentPosts", "key", "nobody", testPassword, 10); f.Code != FaultAuth {
		t.Errorf("unknown user: fault code %d, want %d", f.Code, FaultAuth)
	}
	if _, err := h.store.GetEntry(context.Background(), e.ID); err != nil {
		t.Errorf("a call with a bad password deleted the entry: %v", err)
	}
	if entries, _, err := h.store.ListEntries(context.Background(), store.EntryFilter{}); err != nil || len(entries) != 1 {
		t.Errorf("entries = %d (err %v), want the one fixture", len(entries), err)
	}
	if _, err := os.Stat(filepath.Join(h.dataDir, "enclosures")); !os.IsNotExist(err) {
		t.Errorf("a call with a bad password made the enclosures folder (err %v)", err)
	}

	// The right password gets through.
	if v := h.array("metaWeblog.getRecentPosts", "key", u.Username, testPassword, 10); len(v) != 1 {
		t.Errorf("getRecentPosts with the right password = %d entries, want 1", len(v))
	}

	// An unknown method is its own fault, not a silent empty packet.
	if f := h.fault("metaWeblog.notAMethod", "key", u.Username, testPassword); f.Code != FaultNoMethod {
		t.Errorf("unknown method: fault code %d, want %d", f.Code, FaultNoMethod)
	}

	// So is a packet that is not XML-RPC at all.
	req := httptest.NewRequest(http.MethodPost, "/xmlrpc", strings.NewReader("<not-a-call/>"))
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	resp, err := DecodeResponse(rec.Body.Bytes())
	if err != nil {
		t.Fatalf("DecodeResponse: %v", err)
	}
	if resp.Fault == nil || resp.Fault.Code != FaultParse {
		t.Errorf("a malformed packet = %#v, want a parse fault", resp.Fault)
	}
}

// TestFP_X03_GetUsersBlogs is X03: one blog, under both names.
func TestFP_X03_GetUsersBlogs(t *testing.T) {
	h := newHarness(t)
	u := h.user("ray")
	if err := h.settings.Set(context.Background(), map[string]string{"blogtitle": "Ray's Blog"}); err != nil {
		t.Fatalf("set blogtitle: %v", err)
	}

	for _, method := range []string{"blogger.getUsersBlogs", "metaWeblog.getUsersBlogs"} {
		arr := h.array(method, "key", u.Username, testPassword)
		if len(arr) != 1 {
			t.Fatalf("%s: %d blogs, want 1", method, len(arr))
		}
		st, ok := arr[0].(Struct)
		if !ok {
			t.Fatalf("%s: %#v, want a struct", method, arr[0])
		}
		if got := st.Str("blogid"); got != blogID {
			t.Errorf("%s: blogid = %q, want %q", method, got, blogID)
		}
		if got := st.Str("blogName"); got != "Ray's Blog" {
			t.Errorf("%s: blogName = %q, want the blogtitle setting", method, got)
		}
		if got := st.Str("url"); got != testBase {
			t.Errorf("%s: url = %q, want %q", method, got, testBase)
		}
	}
}

// TestFP_X04_GetCategoriesAndMtCategoryList is X04: the two spellings of
// the category list, with the keys the as-is answered with.
func TestFP_X04_GetCategoriesAndMtCategoryList(t *testing.T) {
	h := newHarness(t)
	u := h.user("ray")
	cf := h.category("ColdFusion", "coldfusion")
	h.category("Go", "go")

	arr := h.array("metaWeblog.getCategories", "key", u.Username, testPassword)
	if len(arr) != 2 {
		t.Fatalf("getCategories = %d categories, want 2", len(arr))
	}
	st := arr[0].(Struct)
	for key, want := range map[string]string{
		"description":  cf.Name,
		"title":        cf.Name,
		"categoryName": cf.Name,
		"categoryid":   cf.ID,
		"htmlUrl":      testBase + "/coldfusion",
		"rssUrl":       testBase + "/rss?mode=full&mode2=cat&catid=" + cf.ID,
	} {
		if got := st.Str(key); got != want {
			t.Errorf("getCategories %s = %q, want %q", key, got, want)
		}
	}

	mt := h.array("mt.getCategoryList", "key", u.Username, testPassword)
	if len(mt) != 2 {
		t.Fatalf("mt.getCategoryList = %d categories, want 2", len(mt))
	}
	mtFirst := mt[0].(Struct)
	if len(mtFirst) != 2 {
		t.Errorf("mt.getCategoryList carries %d keys, want categoryName and categoryId only: %#v", len(mtFirst), mtFirst)
	}
	if got := mtFirst.Str("categoryName"); got != cf.Name {
		t.Errorf("mt categoryName = %q, want %q", got, cf.Name)
	}
	if got := mtFirst.Str("categoryId"); got != cf.ID {
		t.Errorf("mt categoryId = %q, want %q", got, cf.ID)
	}
}

// TestFP_X05_GetRecentPostsIncludesDraftsAndGetPost is X05: the author
// sees their drafts and their scheduled entries over XML-RPC, newest
// first, capped by numberOfPosts (PLAN §11 "Three entry states").
func TestFP_X05_GetRecentPostsIncludesDraftsAndGetPost(t *testing.T) {
	h := newHarness(t)
	u := h.user("ray")
	h.user("sim")
	cat := h.category("ColdFusion", "coldfusion")
	now := time.Now().UTC().Truncate(time.Second)

	live := h.entry(&store.Entry{Title: "Live", Alias: "live", Body: "shown", MoreBody: "the rest",
		Posted: now.Add(-48 * time.Hour), Username: u.Username, AllowComments: true, Released: true})
	draft := h.entry(&store.Entry{Title: "Draft", Alias: "draft", Body: "not yet",
		Posted: now.Add(-24 * time.Hour), Username: u.Username, Released: false})
	h.entry(&store.Entry{Title: "Scheduled", Alias: "scheduled", Body: "later",
		Posted: now.Add(24 * time.Hour), Username: u.Username, AllowComments: true, Released: true})
	h.entry(&store.Entry{Title: "Someone else's", Alias: "sims", Body: "hers",
		Posted: now, Username: "sim", Released: true})
	if err := h.store.SetEntryCategories(context.Background(), live.ID, []string{cat.ID}); err != nil {
		t.Fatalf("SetEntryCategories: %v", err)
	}

	arr := h.array("metaWeblog.getRecentPosts", "key", u.Username, testPassword, 10)
	titles := make([]string, 0, len(arr))
	for _, item := range arr {
		titles = append(titles, item.(Struct).Str("title"))
	}
	want := []string{"Scheduled", "Draft", "Live"}
	if strings.Join(titles, ",") != strings.Join(want, ",") {
		t.Fatalf("getRecentPosts = %v, want %v (newest first, drafts included, other authors out)", titles, want)
	}

	// numberOfPosts caps the list.
	if capped := h.array("metaWeblog.getRecentPosts", "key", u.Username, testPassword, 2); len(capped) != 2 {
		t.Errorf("numberOfPosts = 2 returned %d entries", len(capped))
	}

	// The live entry's struct is the as-is's shape.
	var post Struct
	for _, item := range arr {
		if st := item.(Struct); st.Str("title") == "Live" {
			post = st
		}
	}
	checks := map[string]string{
		"postid":            live.ID,
		"userid":            u.Username,
		"description":       "shown" + moreTag + "the rest",
		"link":              testBase + "/" + live.Posted.Format("2006/1/2") + "/live",
		"permaLink":         testBase + "/" + live.Posted.Format("2006/1/2") + "/live",
		"mt_excerpt":        "",
		"mt_text_more":      "",
		"mt_convert_breaks": "__default__",
		"mt_keywords":       "",
	}
	for key, want := range checks {
		if got := post.Str(key); got != want {
			t.Errorf("post %s = %q, want %q", key, got, want)
		}
	}
	if v, _ := post.Get("mt_allow_comments"); v != 1 {
		t.Errorf("mt_allow_comments = %#v, want 1", v)
	}
	if v, _ := post.Get("mt_allow_pings"); v != 1 {
		t.Errorf("mt_allow_pings = %#v, want 1", v)
	}
	if v, _ := post.Get("dateCreated"); v == nil {
		t.Errorf("dateCreated is missing")
	} else if dt, ok := v.(DateTime); !ok || !dt.InZone(time.UTC).Equal(live.Posted) {
		t.Errorf("dateCreated = %#v, want %v", v, live.Posted)
	}
	cats, _ := post.Get("categories")
	if arr, ok := cats.(Array); !ok || len(arr) != 1 || arr[0] != "ColdFusion" {
		t.Errorf("categories = %#v, want the category's name", cats)
	}

	// getPost answers the same struct for one entry, drafts included.
	one := h.structOf("metaWeblog.getPost", draft.ID, u.Username, testPassword)
	if got := one.Str("title"); got != "Draft" {
		t.Errorf("getPost title = %q, want the draft", got)
	}
	if got := one.Str("postid"); got != draft.ID {
		t.Errorf("getPost postid = %q, want %q", got, draft.ID)
	}
	if f := h.fault("metaWeblog.getPost", "no-such-entry", u.Username, testPassword); f.Code != FaultBadParams {
		t.Errorf("getPost of a missing entry: fault %d, want %d", f.Code, FaultBadParams)
	}

	// With the toggle on, the code block comes back escaped (X10).
	coded := h.entry(&store.Entry{Title: "Coded", Alias: "coded", Body: "<code>a < b</code>",
		Posted: now.Add(-72 * time.Hour), Username: u.Username, Released: true})
	resp := h.post("?parseMarkup=true", "metaWeblog.getPost", coded.ID, u.Username, testPassword)
	if resp.Fault != nil {
		t.Fatalf("getPost with parseMarkup: fault %v", resp.Fault)
	}
	if got := resp.Params[0].(Struct).Str("description"); got != "&lt;code&gt;a &lt; b&lt;/code&gt;" {
		t.Errorf("parseMarkup description = %q", got)
	}
	if got := h.structOf("metaWeblog.getPost", coded.ID, u.Username, testPassword).Str("description"); got != "<code>a < b</code>" {
		t.Errorf("description without parseMarkup = %q, want the stored text", got)
	}
}

// TestFP_X06_NewPostEditPostCategoryTranslationAndFlush is X06.
func TestFP_X06_NewPostEditPostCategoryTranslationAndFlush(t *testing.T) {
	h := newHarness(t)
	u := h.user("ray")
	cf := h.category("ColdFusion", "coldfusion")
	goCat := h.category("Go", "go")
	ctx := context.Background()
	if err := h.settings.Set(ctx, map[string]string{"timezone": "America/Los_Angeles"}); err != nil {
		t.Fatalf("set timezone: %v", err)
	}

	// The cache is filled once; the second fill after the post proves the
	// write flushed it (PLAN §11 "Caching").
	blogCache := cache.New()
	h.module.Cache = blogCache
	fills := 0
	fill := func() (int, error) { fills++; return fills, nil }
	if v, err := cache.Value(blogCache, "home", fill); err != nil || v != 1 {
		t.Fatalf("first fill = %v (err %v), want 1", v, err)
	}
	if v, _ := cache.Value(blogCache, "home", fill); v != 1 {
		t.Fatalf("second read filled again: the cache is not holding anything")
	}

	post := Struct{
		{Name: "title", Value: "Hello & Goodbye"},
		{Name: "description", Value: "the teaser" + moreTag + "the rest"},
		{Name: "categories", Value: Array{"ColdFusion", "No Such Category"}},
		{Name: "dateCreated", Value: NewDateTime(time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC))},
		{Name: "mt_allow_comments", Value: false},
	}
	id, ok := h.value("metaWeblog.newPost", "key", u.Username, testPassword, post, true).(string)
	if !ok || id == "" {
		t.Fatalf("newPost did not answer an id")
	}

	if v, _ := cache.Value(blogCache, "home", fill); v != 2 {
		t.Errorf("the cache was not flushed by newPost: fill count %v", v)
	}

	e := h.reload(id)
	if e.Title != "Hello & Goodbye" {
		t.Errorf("title = %q", e.Title)
	}
	if e.Body != "the teaser" || e.MoreBody != "the rest" {
		t.Errorf("the <more/> split gave body %q, morebody %q", e.Body, e.MoreBody)
	}
	if e.Alias != "Hello--Goodbye" {
		t.Errorf("alias = %q, want MakeTitle's (a bare ampersand is dropped, not turned into `and`)", e.Alias)
	}
	if !e.Released {
		t.Errorf("publish = true did not release the entry")
	}
	if e.AllowComments {
		t.Errorf("mt_allow_comments = false was ignored")
	}
	if e.Username != u.Username {
		t.Errorf("username = %q, want the caller", e.Username)
	}
	// 10:00 in Los Angeles is 18:00 UTC (PLAN §11 "Timezone").
	if want := time.Date(2026, 1, 2, 18, 0, 0, 0, time.UTC); !e.Posted.Equal(want) {
		t.Errorf("posted = %v, want %v (the blog's zone)", e.Posted, want)
	}
	if len(e.Categories) != 1 || e.Categories[0].ID != cf.ID {
		t.Errorf("categories = %#v, want the known name only", e.Categories)
	}

	// editPost answers true, keeps the id, and replaces the categories.
	edit := Struct{
		{Name: "title", Value: "Edited"},
		{Name: "description", Value: "one part only"},
		{Name: "categories", Value: Array{"Go"}},
		{Name: "dateCreated", Value: NewDateTime(time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC))},
	}
	if v := h.value("metaWeblog.editPost", id, u.Username, testPassword, edit, true); v != true {
		t.Fatalf("editPost = %#v, want true", v)
	}
	if v, _ := cache.Value(blogCache, "home", fill); v != 3 {
		t.Errorf("the cache was not flushed by editPost: fill count %v", v)
	}
	e = h.reload(id)
	if e.Title != "Edited" || e.Body != "one part only" || e.MoreBody != "" {
		t.Errorf("after editPost: title %q, body %q, morebody %q", e.Title, e.Body, e.MoreBody)
	}
	if len(e.Categories) != 1 || e.Categories[0].ID != goCat.ID {
		t.Errorf("after editPost categories = %#v, want Go only", e.Categories)
	}
	if e.Alias != "Hello--Goodbye" {
		t.Errorf("editPost changed the alias to %q; the permalink must not move", e.Alias)
	}

	// An unpublished post is a draft, and publishing it later moves its
	// date to now so it does not land half way down the home page.
	draftStruct := Struct{{Name: "title", Value: "Draft"}, {Name: "description", Value: "wip"}}
	draftID := h.value("metaWeblog.newPost", "key", u.Username, testPassword, draftStruct, false).(string)
	if d := h.reload(draftID); d.Released {
		t.Errorf("publish = false released the entry")
	}
	if v := h.value("metaWeblog.editPost", draftID, u.Username, testPassword,
		Struct{{Name: "title", Value: "Draft"}, {Name: "description", Value: "wip"},
			{Name: "dateCreated", Value: NewDateTime(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))}}, true); v != true {
		t.Fatalf("editPost publishing the draft = %#v", v)
	}
	if d := h.reload(draftID); !d.Released || d.Posted.Before(time.Now().UTC().Add(-time.Minute)) {
		t.Errorf("publishing a draft left posted at %v, want now", d.Posted)
	}

	// editPost without a categories key leaves the set alone.
	if v := h.value("metaWeblog.editPost", id, u.Username, testPassword,
		Struct{{Name: "title", Value: "Edited"}, {Name: "description", Value: "one part only"}}, true); v != true {
		t.Fatalf("editPost without categories = %#v", v)
	}
	if e := h.reload(id); len(e.Categories) != 1 {
		t.Errorf("a call with no categories key changed the set: %#v", e.Categories)
	}

	// With the toggle on, what a rich-text client escaped is stored plain.
	resp := h.post("?parseMarkup=true", "metaWeblog.newPost", "key", u.Username, testPassword,
		Struct{{Name: "title", Value: "Coded"}, {Name: "description", Value: "&lt;code&gt;a &lt; b&lt;/code&gt;"}}, true)
	if resp.Fault != nil {
		t.Fatalf("newPost with parseMarkup: fault %v", resp.Fault)
	}
	if e := h.reload(resp.Params[0].(string)); e.Body != "<code>a < b</code>" {
		t.Errorf("parseMarkup body = %q, want the unescaped block", e.Body)
	}
}

// TestFP_X07_DeletePost is X07.
func TestFP_X07_DeletePost(t *testing.T) {
	h := newHarness(t)
	u := h.user("ray")
	e := h.entry(&store.Entry{Title: "Doomed", Alias: "doomed", Body: "body",
		Posted: time.Now().UTC().Truncate(time.Second), Username: u.Username, Released: true})

	blogCache := cache.New()
	h.module.Cache = blogCache
	fills := 0
	fill := func() (int, error) { fills++; return fills, nil }
	if _, err := cache.Value(blogCache, "home", fill); err != nil {
		t.Fatalf("fill: %v", err)
	}

	if v := h.value("blogger.deletePost", "key", e.ID, u.Username, testPassword, true); v != true {
		t.Fatalf("deletePost = %#v, want true", v)
	}
	if _, err := h.store.GetEntry(context.Background(), e.ID); err == nil {
		t.Errorf("the entry is still there")
	}
	if v, _ := cache.Value(blogCache, "home", fill); v != 2 {
		t.Errorf("deletePost did not flush the cache: fill count %v", v)
	}
	if f := h.fault("blogger.deletePost", "key", e.ID, u.Username, testPassword, true); f.Code != FaultBadParams {
		t.Errorf("deleting it twice: fault %d, want %d", f.Code, FaultBadParams)
	}
}

// TestFP_X08_NewMediaObjectWritesUnderMd5AndReturnsURL is X08: the upload
// lands under DATA_DIR/enclosures named for the md5 of the name the
// client gave, and the answer is the URL that serves it.
func TestFP_X08_NewMediaObjectWritesUnderMd5AndReturnsURL(t *testing.T) {
	h := newHarness(t)
	u := h.user("ray")
	bits := []byte("\x89PNG\r\n\x1a\n not really a png")

	media := Struct{
		{Name: "name", Value: "holiday photo.PNG"},
		{Name: "type", Value: "image/png"},
		{Name: "bits", Value: bits},
	}
	st := h.structOf("metaWeblog.newMediaObject", blogID, u.Username, testPassword, media)

	sum := md5.Sum([]byte("holiday photo.PNG"))
	want := hex.EncodeToString(sum[:]) + ".png"
	if got := st.Str("url"); got != testBase+"/enclosures/"+want {
		t.Errorf("url = %q, want %q", got, testBase+"/enclosures/"+want)
	}
	onDisk := filepath.Join(h.dataDir, "enclosures", want)
	got, err := os.ReadFile(onDisk)
	if err != nil {
		t.Fatalf("the upload is not at %s: %v", onDisk, err)
	}
	if !bytes.Equal(got, bits) {
		t.Errorf("the file holds % x, want the bits sent", got)
	}

	// The same name overwrites itself; a different name does not.
	h.structOf("metaWeblog.newMediaObject", blogID, u.Username, testPassword, media)
	if entries, err := os.ReadDir(filepath.Join(h.dataDir, "enclosures")); err != nil || len(entries) != 1 {
		t.Errorf("re-uploading the same name made %d files (err %v), want 1", len(entries), err)
	}

	// A name reaching out of the folder cannot: the md5 is the whole
	// filename and the extension is letters and digits or nothing.
	escaping := Struct{
		{Name: "name", Value: "../../etc/passwd"},
		{Name: "bits", Value: []byte("nope")},
	}
	st = h.structOf("metaWeblog.newMediaObject", blogID, u.Username, testPassword, escaping)
	url := st.Str("url")
	if strings.Contains(url, "..") || strings.Contains(url, "passwd") {
		t.Errorf("url = %q, want the md5 name", url)
	}
	entries, err := os.ReadDir(filepath.Join(h.dataDir, "enclosures"))
	if err != nil || len(entries) != 2 {
		t.Errorf("the escaping upload landed elsewhere: %d files (err %v)", len(entries), err)
	}

	// Bits that are not base64 are a fault, not a zero-byte file.
	if f := h.fault("metaWeblog.newMediaObject", blogID, u.Username, testPassword,
		Struct{{Name: "name", Value: "x.png"}, {Name: "bits", Value: "not base64"}}); f.Code != FaultBadParams {
		t.Errorf("string bits: fault %d, want %d", f.Code, FaultBadParams)
	}
}

// TestFP_X09_GetSetPostCategories is X09: the Movable Type pair, which
// works in ids rather than names.
func TestFP_X09_GetSetPostCategories(t *testing.T) {
	h := newHarness(t)
	u := h.user("ray")
	cf := h.category("ColdFusion", "coldfusion")
	goCat := h.category("Go", "go")
	e := h.entry(&store.Entry{Title: "Post", Alias: "post", Body: "body",
		Posted: time.Now().UTC().Truncate(time.Second), Username: u.Username, Released: true})
	ctx := context.Background()
	if err := h.store.SetEntryCategories(ctx, e.ID, []string{cf.ID}); err != nil {
		t.Fatalf("SetEntryCategories: %v", err)
	}

	arr := h.array("mt.getPostCategories", e.ID, u.Username, testPassword)
	if len(arr) != 1 {
		t.Fatalf("getPostCategories = %d, want 1", len(arr))
	}
	first := arr[0].(Struct)
	if got := first.Str("categoryName"); got != "ColdFusion" {
		t.Errorf("categoryName = %q", got)
	}
	if got := first.Str("categoryId"); got != cf.ID {
		t.Errorf("categoryId = %q, want %q", got, cf.ID)
	}

	set := Array{
		Struct{{Name: "categoryId", Value: goCat.ID}, {Name: "isPrimary", Value: true}},
	}
	if v := h.value("mt.setPostCategories", e.ID, u.Username, testPassword, set); v != true {
		t.Fatalf("setPostCategories = %#v, want true", v)
	}
	after := h.reload(e.ID)
	if len(after.Categories) != 1 || after.Categories[0].ID != goCat.ID {
		t.Errorf("after setPostCategories: %#v, want Go only (the set is replaced)", after.Categories)
	}

	// An empty array clears the set.
	if v := h.value("mt.setPostCategories", e.ID, u.Username, testPassword, Array{}); v != true {
		t.Fatalf("setPostCategories with an empty array = %#v", v)
	}
	if after := h.reload(e.ID); len(after.Categories) != 0 {
		t.Errorf("an empty array left %#v", after.Categories)
	}

	// Without the fourth parameter the as-is fell through to a fault.
	if f := h.fault("mt.setPostCategories", e.ID, u.Username, testPassword); f.Code != FaultBadParams {
		t.Errorf("setPostCategories without a list: fault %d, want %d", f.Code, FaultBadParams)
	}
}

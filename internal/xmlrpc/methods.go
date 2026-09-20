package xmlrpc

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/4scottt/go-blogcfc/internal/render"
	"github.com/4scottt/go-blogcfc/internal/store"
	"github.com/4scottt/go-blogcfc/internal/web"
)

// aliasLimit is the entries table's alias column (PLAN §13).
const aliasLimit = 100

// getUsersBlogs is X03, both spellings. BlogCFC answered one blog and did
// not check the password; here it does (X02).
//
// Params: appkey, username, password.
func (m *Module) getUsersBlogs(r *request) (any, error) {
	if _, err := m.authAt(r, 1, 2); err != nil {
		return nil, err
	}
	return Array{Struct{
		{Name: "url", Value: m.base()},
		{Name: "blogid", Value: blogID},
		{Name: "blogName", Value: m.settings.BlogTitle()},
	}}, nil
}

// getCategories is X04: metaWeblog.getCategories and mt.getCategoryList,
// which differ in the keys they answer with. The spellings are the
// as-is's, `categoryid` and all.
//
// Params: appkey, username, password.
func (m *Module) getCategories(r *request, movableType bool) (any, error) {
	if _, err := m.authAt(r, 1, 2); err != nil {
		return nil, err
	}
	cats, err := m.store.ListCategories(r.ctx)
	if err != nil {
		return nil, fmt.Errorf("xmlrpc: list categories: %w", err)
	}
	out := make(Array, 0, len(cats))
	for _, c := range cats {
		if movableType {
			out = append(out, Struct{
				{Name: "categoryName", Value: c.Name},
				{Name: "categoryId", Value: c.ID},
			})
			continue
		}
		out = append(out, Struct{
			{Name: "description", Value: c.Name},
			{Name: "htmlUrl", Value: web.CategoryURL(m.base(), c)},
			{Name: "rssUrl", Value: m.base() + "/rss?mode=full&mode2=cat&catid=" + c.ID},
			// `title` and `categoryName` are the as-is's additions for
			// MarsEdit; `categoryid` is its lower-case spelling here and
			// `categoryId` in the mt branch.
			{Name: "title", Value: c.Name},
			{Name: "categoryName", Value: c.Name},
			{Name: "categoryid", Value: c.ID},
		})
	}
	return out, nil
}

// getRecentPosts is half of X05: the caller's own entries, newest first,
// drafts and scheduled ones included, capped by numberOfPosts (PLAN §11
// "Three entry states").
//
// Params: appkey, username, password, numberOfPosts.
func (m *Module) getRecentPosts(r *request) (any, error) {
	u, err := m.authAt(r, 1, 2)
	if err != nil {
		return nil, err
	}
	limit := r.params.intOr(3, m.settings.MaxEntries())
	entries, _, err := m.store.ListEntries(r.ctx, store.EntryFilter{
		Username: u.Username,
		Sort:     "posted",
		Desc:     true,
		Limit:    limit,
	})
	if err != nil {
		return nil, fmt.Errorf("xmlrpc: list entries: %w", err)
	}
	out := make(Array, 0, len(entries))
	for i := range entries {
		out = append(out, m.postStruct(&entries[i], r.parseMarkup))
	}
	return out, nil
}

// getPost is the other half of X05.
//
// Params: postid, username, password.
func (m *Module) getPost(r *request) (any, error) {
	if _, err := m.authAt(r, 1, 2); err != nil {
		return nil, err
	}
	id, err := r.params.str(0)
	if err != nil {
		return nil, err
	}
	e, err := m.entry(r, id)
	if err != nil {
		return nil, err
	}
	return m.postStruct(e, r.parseMarkup), nil
}

// postStruct is one entry as the clients read it. The keys and their
// values are the as-is's, with the `<more/>` halves re-joined into
// `description` (PLAN §11): BlogCFC chose between that and `mt_text_more`
// from an application-scope flag it set when a client last asked for
// categories, which is state a stateless endpoint should not keep.
func (m *Module) postStruct(e *store.Entry, parseMarkup bool) Struct {
	description := e.Body
	if e.MoreBody != "" {
		description = e.Body + moreTag + e.MoreBody
	}
	if parseMarkup {
		description = escapeMarkup(description)
	}
	allow := 0
	if e.AllowComments {
		allow = 1
	}
	names := make(Array, 0, len(e.Categories))
	for _, c := range e.Categories {
		names = append(names, c.Name)
	}
	return Struct{
		{Name: "title", Value: e.Title},
		{Name: "dateCreated", Value: NewDateTime(e.Posted.In(m.settings.Timezone()))},
		{Name: "userid", Value: e.Username},
		{Name: "postid", Value: e.ID},
		{Name: "description", Value: description},
		{Name: "link", Value: web.EntryURL(m.base(), *e, m.settings.Timezone())},
		{Name: "permaLink", Value: web.EntryURL(m.base(), *e, m.settings.Timezone())},
		{Name: "mt_excerpt", Value: ""},
		{Name: "mt_text_more", Value: ""},
		{Name: "mt_allow_comments", Value: allow},
		{Name: "mt_allow_pings", Value: 1},
		{Name: "mt_convert_breaks", Value: "__default__"},
		{Name: "mt_keywords", Value: ""},
		{Name: "categories", Value: names},
	}
}

// savePost is X06: newPost and editPost, which differ only in their first
// parameter and in what they answer with.
//
// newPost:  appkey, username, password, struct, publish -> the new id.
// editPost: postid, username, password, struct, publish -> true.
func (m *Module) savePost(r *request, editing bool) (any, error) {
	u, err := m.authAt(r, 1, 2)
	if err != nil {
		return nil, err
	}
	in, err := r.params.structAt(3)
	if err != nil {
		return nil, err
	}
	publish := r.params.boolOr(4, true)

	var existing *store.Entry
	if editing {
		id, err := r.params.str(0)
		if err != nil {
			return nil, err
		}
		if existing, err = m.entry(r, id); err != nil {
			return nil, err
		}
	}

	description := in.Str("description")
	if r.parseMarkup {
		description = unescapeMarkup(description)
	}
	description = fixEditorEntities(description)

	body, more := splitMore(description)
	if more == "" {
		// A Movable Type client sends the second half in its own key
		// rather than behind a <more/>.
		more = in.Str("mt_text_more")
	}

	e := existing
	if e == nil {
		e = &store.Entry{Username: u.Username, SendEmail: false}
	}
	e.Title = in.Str("title")
	e.Body = body
	e.MoreBody = more
	e.AllowComments = true
	if v, ok := in.Get("mt_allow_comments"); ok {
		if b, ok := toBool(v); ok {
			e.AllowComments = b
		}
	}

	posted, hasPosted, err := m.postedFrom(in)
	if err != nil {
		return nil, err
	}
	switch {
	case hasPosted:
		e.Posted = posted
	case !editing:
		e.Posted = time.Now().UTC().Truncate(time.Second)
	}
	// A draft published with a date already past is posted now, so it does
	// not appear half way down the home page: the as-is's rule here and in
	// admin/entry.cfm alike (PLAN §9 A06).
	releasedBefore := existing != nil && existing.Released
	if !releasedBefore && publish && (e.Posted.IsZero() || e.Posted.Before(time.Now().UTC())) {
		e.Posted = time.Now().UTC().Truncate(time.Second)
	}
	e.Released = publish

	if existing == nil {
		e.Alias = clip(render.MakeTitle(e.Title), aliasLimit)
		if err := m.store.CreateEntry(r.ctx, e); err != nil {
			if errors.Is(err, store.ErrDuplicate) {
				return nil, Fault{FaultBadParams, "another entry already uses the alias " + e.Alias}
			}
			return nil, fmt.Errorf("xmlrpc: create entry: %w", err)
		}
	} else if err := m.store.UpdateEntry(r.ctx, e); err != nil {
		return nil, fmt.Errorf("xmlrpc: update entry: %w", err)
	}

	// Categories arrive by name, in `categories` or in a lone `category`.
	// A name the blog does not know is ignored, as the as-is's lookup
	// ignored it by returning an empty id.
	if names, ok := categoryNames(in); ok {
		ids, err := m.categoryIDs(r, names)
		if err != nil {
			return nil, err
		}
		if err := m.store.SetEntryCategories(r.ctx, e.ID, ids); err != nil {
			return nil, fmt.Errorf("xmlrpc: set entry categories: %w", err)
		}
	}

	m.flush()
	if editing {
		return true, nil
	}
	return e.ID, nil
}

// postedFrom reads `dateCreated`, or `pubDate` behind it, in the blog's
// zone when the client sent no offset (PLAN §11 "Timezone").
func (m *Module) postedFrom(in Struct) (time.Time, bool, error) {
	for _, key := range []string{"dateCreated", "pubDate", "date_created_gmt"} {
		v, ok := in.Get(key)
		if !ok {
			continue
		}
		switch t := v.(type) {
		case DateTime:
			if key == "date_created_gmt" {
				return t.Time.UTC(), true, nil
			}
			return t.InZone(m.settings.Timezone()), true, nil
		case string:
			if strings.TrimSpace(t) == "" {
				continue
			}
			parsed, err := parseDateTime(t)
			if err != nil {
				return time.Time{}, false, Fault{FaultBadParams, err.Error()}
			}
			return parsed.InZone(m.settings.Timezone()), true, nil
		}
	}
	return time.Time{}, false, nil
}

// deletePost is X07.
//
// Params: appkey, postid, username, password, publish.
func (m *Module) deletePost(r *request) (any, error) {
	if _, err := m.authAt(r, 2, 3); err != nil {
		return nil, err
	}
	id, err := r.params.str(1)
	if err != nil {
		return nil, err
	}
	if _, err := m.entry(r, id); err != nil {
		return nil, err
	}
	if err := m.store.DeleteEntries(r.ctx, []string{id}); err != nil {
		return nil, fmt.Errorf("xmlrpc: delete entry: %w", err)
	}
	m.flush()
	return true, nil
}

// extPattern keeps a client's extension to letters and digits: the name
// comes from the far end and is about to become a path.
var extPattern = regexp.MustCompile(`^[A-Za-z0-9]{1,10}$`)

// newMediaObject is X08: the upload a rich-text client makes before it
// posts the entry that links to it. The file is named for the md5 of the
// name the client gave, which is how the as-is stopped two uploads called
// `image.png` overwriting each other while a re-upload of the same file
// still replaced itself (dgs, 2011-09-08).
//
// Params: blogid, username, password, struct{name, type, bits}.
func (m *Module) newMediaObject(r *request) (any, error) {
	if _, err := m.authAt(r, 1, 2); err != nil {
		return nil, err
	}
	in, err := r.params.structAt(3)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Str("name"))
	if name == "" {
		return nil, Fault{FaultBadParams, "the media object has no name"}
	}
	raw, ok := in.Get("bits")
	if !ok {
		return nil, Fault{FaultBadParams, "the media object has no bits"}
	}
	bits, ok := raw.([]byte)
	if !ok {
		return nil, Fault{FaultBadParams, "the media object's bits must be base64"}
	}

	sum := md5.Sum([]byte(name))
	filename := hex.EncodeToString(sum[:])
	if ext := mediaExt(name); ext != "" {
		filename += "." + ext
	}

	dir := filepath.Join(m.cfg.DataDir, "enclosures")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("xmlrpc: enclosures folder: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), bits, 0o644); err != nil {
		return nil, fmt.Errorf("xmlrpc: write %s: %w", filename, err)
	}
	return Struct{{Name: "url", Value: m.base() + "/enclosures/" + filename}}, nil
}

// mediaExt is the as-is's `listLast(name, ".")`, with the name reduced to
// its last path segment and the extension held to letters and digits
// first: the value is a client's and it is about to be part of a path.
func mediaExt(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	name = path.Base(strings.TrimSpace(name))
	idx := strings.LastIndex(name, ".")
	if idx < 0 {
		return ""
	}
	ext := name[idx+1:]
	if !extPattern.MatchString(ext) {
		return ""
	}
	return strings.ToLower(ext)
}

// getPostCategories is half of X09.
//
// Params: postid, username, password.
func (m *Module) getPostCategories(r *request) (any, error) {
	if _, err := m.authAt(r, 1, 2); err != nil {
		return nil, err
	}
	id, err := r.params.str(0)
	if err != nil {
		return nil, err
	}
	e, err := m.entry(r, id)
	if err != nil {
		return nil, err
	}
	out := make(Array, 0, len(e.Categories))
	for _, c := range e.Categories {
		out = append(out, Struct{
			{Name: "categoryName", Value: c.Name},
			{Name: "categoryId", Value: c.ID},
		})
	}
	return out, nil
}

// setPostCategories is the other half of X09: the client sends ids, not
// names, and the set replaces what was there.
//
// Params: postid, username, password, array of struct{categoryId}.
func (m *Module) setPostCategories(r *request) (any, error) {
	if _, err := m.authAt(r, 1, 2); err != nil {
		return nil, err
	}
	id, err := r.params.str(0)
	if err != nil {
		return nil, err
	}
	if _, err := m.entry(r, id); err != nil {
		return nil, err
	}
	list, err := r.params.arrayAt(3)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for _, item := range list {
		switch v := item.(type) {
		case Struct:
			if cid := strings.TrimSpace(v.Str("categoryId")); cid != "" {
				ids = append(ids, cid)
			}
		case string:
			if cid := strings.TrimSpace(v); cid != "" {
				ids = append(ids, cid)
			}
		}
	}
	if err := m.store.SetEntryCategories(r.ctx, id, ids); err != nil {
		return nil, fmt.Errorf("xmlrpc: set entry categories: %w", err)
	}
	m.flush()
	return true, nil
}

// entry loads one entry, turning a missing row into a fault rather than a
// 500.
func (m *Module) entry(r *request, id string) (*store.Entry, error) {
	if strings.TrimSpace(id) == "" {
		return nil, Fault{FaultBadParams, "no post id"}
	}
	e, err := m.store.GetEntry(r.ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, Fault{FaultBadParams, "no entry with id " + id}
		}
		return nil, fmt.Errorf("xmlrpc: get entry %s: %w", id, err)
	}
	return e, nil
}

// categoryNames reads the names a client sent, and reports whether it
// sent the key at all: editPost without one leaves the categories alone.
func categoryNames(in Struct) ([]string, bool) {
	if v, ok := in.Get("categories"); ok {
		var names []string
		if arr, ok := v.(Array); ok {
			for _, item := range arr {
				if s := strings.TrimSpace(asString(item)); s != "" {
					names = append(names, s)
				}
			}
		}
		return names, true
	}
	if v, ok := in.Get("category"); ok {
		s := strings.TrimSpace(asString(v))
		if s == "" {
			return nil, false
		}
		// A lone `category` is BlogCFC's fallback and it is a list.
		var names []string
		for _, part := range strings.Split(s, ",") {
			if p := strings.TrimSpace(part); p != "" {
				names = append(names, p)
			}
		}
		return names, true
	}
	return nil, false
}

// categoryIDs translates category names to ids. An unknown name is
// dropped: the as-is's getCategoryByName answered an empty id for one and
// the empty id then fell out of the list it was appended to, so a client
// inventing a category never created one.
func (m *Module) categoryIDs(r *request, names []string) ([]string, error) {
	if len(names) == 0 {
		return nil, nil
	}
	cats, err := m.store.ListCategories(r.ctx)
	if err != nil {
		return nil, fmt.Errorf("xmlrpc: list categories: %w", err)
	}
	var ids []string
	for _, name := range names {
		for _, c := range cats {
			if strings.EqualFold(c.Name, name) {
				ids = append(ids, c.ID)
				break
			}
		}
	}
	return ids, nil
}

// splitMore cuts the client's one field into the two the store keeps. A
// leading <more/> is not a split: the part before it is what the lists
// show, and the as-is's `moreStart gt 1` said the same thing.
func splitMore(description string) (body, more string) {
	idx := strings.Index(strings.ToLower(description), moreTag)
	if idx <= 0 {
		return description, ""
	}
	return strings.TrimSpace(description[:idx]), strings.TrimSpace(description[idx+len(moreTag):])
}

// editorEntities are the characters Windows Live Writer sends raw and
// BlogCFC replaced with their HTML entities on the way in (dgs,
// 2011-09-08). The Windows-1252 code points are the ones that arrive when
// a client mislabels its encoding.
var editorEntities = strings.NewReplacer(
	"…", "&#8230;",
	"—", "&#8212;",
	"\u0097", "&#8212;",
	"–", "&#8211;",
	"\u0096", "&#8211;",
)

func fixEditorEntities(s string) string { return editorEntities.Replace(s) }

// clip cuts a string to n runes, never mid-character.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

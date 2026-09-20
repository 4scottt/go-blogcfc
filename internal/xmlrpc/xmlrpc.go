package xmlrpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/4scottt/go-blogcfc/internal/auth"
	"github.com/4scottt/go-blogcfc/internal/cache"
	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/store"
)

// moreTag splits an entry into the part every list shows and the part the
// permalink adds (PLAN §11 "The <more/> split"). The clients send it
// inside `description`; the store keeps the two halves apart.
const moreTag = "<more/>"

// maxBody bounds a packet. newMediaObject carries a whole upload as
// base64, so the limit is the admin's upload limit with room for the
// encoding, not a few kilobytes.
const maxBody = 48 << 20

// blogID is the one blog's id. BlogCFC was multi-tenant in the schema and
// single-blog in practice; the rewrite dropped the tenant column
// (PLAN §7), so every client is told about blog 1.
const blogID = "1"

// Fault codes. The as-is had none worth porting: every failure fell
// through to `type = "responsefault"` with an empty `result`, so the
// packet carried `<int></int>` and an empty faultString and no client
// could tell a bad password from an unknown method. The shape is kept
// (always a fault, always faultCode and faultString) and the codes are
// filled in: 4 is the Blogger API's "invalid login", the negatives are
// the XML-RPC fault-code interop proposal's.
const (
	FaultAuth      = 4
	FaultParse     = -32700
	FaultNoMethod  = -32601
	FaultBadParams = -32602
	FaultServer    = -32500
)

// Module serves POST /xmlrpc.
type Module struct {
	// Cache is the blog's one in-process cache; a write over XML-RPC
	// flushes it exactly as an admin save does (PLAN §11 "Caching").
	// main.go sets it, and nil means nothing is cached.
	Cache *cache.Cache

	// Release runs the release side effects (subscriber mail, pings, the
	// sweep's mark) after newPost/editPost, exactly as an admin save does
	// through the same hook (PLAN §11 "Release side effects"). main.go sets
	// it to the releaser's OnEntrySaved; nil means no side effects, which
	// is what the tests want.
	Release func(ctx context.Context, e *store.Entry, releasedBefore bool) error

	cfg      *config.Config
	store    *store.Store
	settings *config.Settings
}

// New builds the module.
func New(cfg *config.Config, st *store.Store, settings *config.Settings) *Module {
	return &Module{cfg: cfg, store: st, settings: settings}
}

// Routes registers the one endpoint (PLAN §8).
func (m *Module) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /xmlrpc", m.handle)
}

// request is one decoded call and the switches that ride on the URL.
type request struct {
	ctx         context.Context
	params      params
	parseMarkup bool
}

func (m *Module) handle(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		m.writeFault(w, Fault{FaultParse, "the request body could not be read: " + err.Error()})
		return
	}
	if len(body) == 0 {
		// The as-is aborted on an empty body and wrote nothing at all. A
		// fault says the same thing in a packet a client can read.
		m.writeFault(w, Fault{FaultParse, "empty request body"})
		return
	}
	call, err := DecodeCall(body)
	if err != nil {
		m.writeFault(w, Fault{FaultParse, err.Error()})
		return
	}

	req := &request{
		ctx:         r.Context(),
		params:      params(call.Params),
		parseMarkup: cfBool(r.URL.Query().Get("parseMarkup")),
	}

	value, err := m.dispatch(call.Method, req)
	if err != nil {
		var f Fault
		if errors.As(err, &f) {
			m.writeFault(w, f)
			return
		}
		slog.Error("xmlrpc: call failed", "method", call.Method, "error", err)
		m.writeFault(w, Fault{FaultServer, err.Error()})
		return
	}
	out, err := EncodeResponse(value)
	if err != nil {
		slog.Error("xmlrpc: response could not be encoded", "method", call.Method, "error", err)
		m.writeFault(w, Fault{FaultServer, err.Error()})
		return
	}
	writeXML(w, http.StatusOK, out)
}

// dispatch is xmlrpc.cfm's cfswitch: the twelve methods BlogCFC answers
// (PLAN §7, §9 X03-X09). Anything else is a fault.
func (m *Module) dispatch(method string, r *request) (any, error) {
	switch method {
	case "blogger.getUsersBlogs", "metaWeblog.getUsersBlogs":
		return m.getUsersBlogs(r)
	case "metaWeblog.getCategories":
		return m.getCategories(r, false)
	case "mt.getCategoryList":
		return m.getCategories(r, true)
	case "metaWeblog.getRecentPosts":
		return m.getRecentPosts(r)
	case "metaWeblog.getPost":
		return m.getPost(r)
	case "metaWeblog.newPost":
		return m.savePost(r, false)
	case "metaWeblog.editPost":
		return m.savePost(r, true)
	case "blogger.deletePost":
		return m.deletePost(r)
	case "metaWeblog.newMediaObject":
		return m.newMediaObject(r)
	case "mt.getPostCategories":
		return m.getPostCategories(r)
	case "mt.setPostCategories":
		return m.setPostCategories(r)
	}
	return nil, Fault{FaultNoMethod, "unknown method: " + method}
}

func (m *Module) writeFault(w http.ResponseWriter, f Fault) {
	// A fault is a successful HTTP response carrying an application
	// error; that is what the clients parse, so the status stays 200.
	writeXML(w, http.StatusOK, EncodeFault(f))
}

func writeXML(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "text/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

// authenticate is X02: every method checks the username and password
// against the store before it does anything. BlogCFC left
// `blogger.getUsersBlogs` unchecked and answered `newMediaObject` and
// `deletePost` with a bare `false`; here all twelve authenticate and a
// failure is a fault.
func (m *Module) authenticate(ctx context.Context, username, password string) (*store.User, error) {
	if strings.TrimSpace(username) == "" {
		return nil, Fault{FaultAuth, "Invalid username or password."}
	}
	u, err := m.store.GetUser(ctx, username)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, Fault{FaultAuth, "Invalid username or password."}
		}
		return nil, fmt.Errorf("xmlrpc: look up %s: %w", username, err)
	}
	if !auth.CheckPassword(u.PasswordHash, password) {
		return nil, Fault{FaultAuth, "Invalid username or password."}
	}
	return u, nil
}

// authAt authenticates with the username and password at these two
// positions, which move about from method to method.
func (m *Module) authAt(r *request, userIdx, passIdx int) (*store.User, error) {
	username, err := r.params.str(userIdx)
	if err != nil {
		return nil, err
	}
	password, err := r.params.str(passIdx)
	if err != nil {
		return nil, err
	}
	return m.authenticate(r.ctx, username, password)
}

// flush drops the caches after a write, as every admin save does
// (PLAN §11 "Caching", §9 A29). Cache.Flush is nil-safe.
func (m *Module) flush() { m.Cache.Flush() }

// base is the blog's own base URL: every link comes from BLOG_BASE_URL,
// never from the request (PLAN §6).
func (m *Module) base() string { return strings.TrimRight(m.cfg.BlogBaseURL, "/") }

// params is a call's positional arguments.
type params []any

func (p params) at(i int) (any, error) {
	if i < 0 || i >= len(p) {
		return nil, Fault{FaultBadParams, fmt.Sprintf("this method needs at least %d parameters, got %d", i+1, len(p))}
	}
	return p[i], nil
}

func (p params) str(i int) (string, error) {
	v, err := p.at(i)
	if err != nil {
		return "", err
	}
	return asString(v), nil
}

func (p params) intOr(i, def int) int {
	v, err := p.at(i)
	if err != nil {
		return def
	}
	n, ok := toInt(v)
	if !ok || n <= 0 {
		return def
	}
	return n
}

func (p params) boolOr(i int, def bool) bool {
	v, err := p.at(i)
	if err != nil {
		return def
	}
	b, ok := toBool(v)
	if !ok {
		return def
	}
	return b
}

func (p params) structAt(i int) (Struct, error) {
	v, err := p.at(i)
	if err != nil {
		return nil, err
	}
	st, ok := v.(Struct)
	if !ok {
		return nil, Fault{FaultBadParams, fmt.Sprintf("parameter %d must be a struct", i+1)}
	}
	return st, nil
}

func (p params) arrayAt(i int) (Array, error) {
	v, err := p.at(i)
	if err != nil {
		return nil, err
	}
	arr, ok := v.(Array)
	if !ok {
		return nil, Fault{FaultBadParams, fmt.Sprintf("parameter %d must be an array", i+1)}
	}
	return arr, nil
}

// cfBool reads a URL switch the way `<cfparam type="boolean">` did.
func cfBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes":
		return true
	}
	return false
}

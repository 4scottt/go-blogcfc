package xmlrpc

import (
	"strings"
	"testing"
	"time"
)

// TestFP_X01_CodecRoundTripFaultsAndUTF8Prolog covers the codec on its
// own (PLAN §9 X01): every value type the clients use survives a round
// trip, a fault decodes as a fault, and the prolog says UTF-8 -- the bug
// the as-is shipped, which wrote `encoding="ISO-8859-1"` above UTF-8
// bytes (PLAN §7).
func TestFP_X01_CodecRoundTripFaultsAndUTF8Prolog(t *testing.T) {
	t.Run("call round trip", func(t *testing.T) {
		posted := time.Date(2011, 9, 8, 14, 5, 6, 0, time.UTC)
		params := []any{
			"a string with <angles> & an ampersand and a ‘quote’",
			42,
			true,
			false,
			3.5,
			NewDateTime(posted),
			[]byte{0x00, 0x01, 0xfe, 0xff},
			Array{"one", 2, false},
			Struct{
				{Name: "title", Value: "Hello"},
				{Name: "categories", Value: Array{"Coldfusion", "Go"}},
				{Name: "nested", Value: Struct{{Name: "deep", Value: 7}}},
			},
		}
		packet, err := EncodeCall("metaWeblog.newPost", params...)
		if err != nil {
			t.Fatalf("EncodeCall: %v", err)
		}
		if !strings.HasPrefix(string(packet), Prolog) {
			t.Fatalf("call prolog = %.60q, want %q first", packet, Prolog)
		}
		call, err := DecodeCall(packet)
		if err != nil {
			t.Fatalf("DecodeCall: %v", err)
		}
		if call.Method != "metaWeblog.newPost" {
			t.Fatalf("method = %q", call.Method)
		}
		if len(call.Params) != len(params) {
			t.Fatalf("params = %d, want %d", len(call.Params), len(params))
		}
		if got := call.Params[0].(string); got != params[0] {
			t.Errorf("string param = %q, want %q", got, params[0])
		}
		if got := call.Params[1].(int); got != 42 {
			t.Errorf("int param = %d, want 42", got)
		}
		if got := call.Params[2].(bool); !got {
			t.Errorf("boolean param = false, want true")
		}
		if got := call.Params[3].(bool); got {
			t.Errorf("boolean param = true, want false")
		}
		if got := call.Params[4].(float64); got != 3.5 {
			t.Errorf("double param = %v, want 3.5", got)
		}
		if got := call.Params[5].(DateTime); !got.Time.Equal(posted) {
			t.Errorf("dateTime param = %v, want %v", got.Time, posted)
		}
		if got := call.Params[6].([]byte); string(got) != string([]byte{0x00, 0x01, 0xfe, 0xff}) {
			t.Errorf("base64 param = % x", got)
		}
		arr, ok := call.Params[7].(Array)
		if !ok || len(arr) != 3 || arr[0] != "one" || arr[1] != 2 || arr[2] != false {
			t.Errorf("array param = %#v", call.Params[7])
		}
		st, ok := call.Params[8].(Struct)
		if !ok {
			t.Fatalf("struct param = %#v", call.Params[8])
		}
		if st.Str("title") != "Hello" {
			t.Errorf("struct title = %q", st.Str("title"))
		}
		cats, _ := st.Get("categories")
		if arr, ok := cats.(Array); !ok || len(arr) != 2 || arr[1] != "Go" {
			t.Errorf("struct categories = %#v", cats)
		}
		nested, _ := st.Get("nested")
		if inner, ok := nested.(Struct); !ok || inner[0].Value != 7 {
			t.Errorf("nested struct = %#v", nested)
		}
	})

	t.Run("response round trip and prolog", func(t *testing.T) {
		packet, err := EncodeResponse(Struct{{Name: "url", Value: "http://blog.example/enclosures/x.png"}})
		if err != nil {
			t.Fatalf("EncodeResponse: %v", err)
		}
		if !strings.HasPrefix(string(packet), Prolog) {
			t.Fatalf("response prolog = %.60q, want %q first", packet, Prolog)
		}
		if strings.Contains(string(packet), "ISO-8859-1") {
			t.Fatalf("the response still declares ISO-8859-1: %.60q", packet)
		}
		resp, err := DecodeResponse(packet)
		if err != nil {
			t.Fatalf("DecodeResponse: %v", err)
		}
		if resp.Fault != nil {
			t.Fatalf("fault = %v, want none", resp.Fault)
		}
		if len(resp.Params) != 1 {
			t.Fatalf("params = %d, want 1", len(resp.Params))
		}
		if got := resp.Params[0].(Struct).Str("url"); got != "http://blog.example/enclosures/x.png" {
			t.Errorf("url = %q", got)
		}
	})

	t.Run("utf-8 survives", func(t *testing.T) {
		const text = "Grüße, 日本語, emoji 🎉"
		packet, err := EncodeResponse(text)
		if err != nil {
			t.Fatalf("EncodeResponse: %v", err)
		}
		resp, err := DecodeResponse(packet)
		if err != nil {
			t.Fatalf("DecodeResponse: %v", err)
		}
		if got := resp.Params[0].(string); got != text {
			t.Errorf("round trip = %q, want %q", got, text)
		}
	})

	t.Run("fault", func(t *testing.T) {
		packet := EncodeFault(Fault{Code: FaultAuth, String: `Invalid username or password.`})
		if !strings.HasPrefix(string(packet), Prolog) {
			t.Fatalf("fault prolog = %.60q", packet)
		}
		resp, err := DecodeResponse(packet)
		if err != nil {
			t.Fatalf("DecodeResponse: %v", err)
		}
		if resp.Fault == nil {
			t.Fatalf("fault = nil, want one")
		}
		if resp.Fault.Code != FaultAuth || resp.Fault.String != "Invalid username or password." {
			t.Errorf("fault = %+v", *resp.Fault)
		}
		if len(resp.Params) != 0 {
			t.Errorf("a fault carried params: %#v", resp.Params)
		}
	})

	t.Run("untyped value is a string", func(t *testing.T) {
		call, err := DecodeCall([]byte(`<?xml version="1.0"?><methodCall><methodName>blogger.getUsersBlogs</methodName>` +
			`<params><param><value>appkey</value></param>` +
			`<param><value><i4>7</i4></value></param></params></methodCall>`))
		if err != nil {
			t.Fatalf("DecodeCall: %v", err)
		}
		if call.Params[0] != "appkey" {
			t.Errorf("untyped value = %#v, want the string", call.Params[0])
		}
		if call.Params[1] != 7 {
			t.Errorf("i4 = %#v, want 7", call.Params[1])
		}
	})

	t.Run("dateTime spellings", func(t *testing.T) {
		blogZone := time.FixedZone("test", -5*3600)
		cases := []struct {
			raw     string
			want    time.Time
			hasZone bool
		}{
			{"20110908T14:05:06", time.Date(2011, 9, 8, 19, 5, 6, 0, time.UTC), false},
			{"2011-09-08T14:05:06", time.Date(2011, 9, 8, 19, 5, 6, 0, time.UTC), false},
			{"20110908T14:05:06Z", time.Date(2011, 9, 8, 14, 5, 6, 0, time.UTC), true},
			{"2011-09-08T14:05:06+02:00", time.Date(2011, 9, 8, 12, 5, 6, 0, time.UTC), true},
		}
		for _, c := range cases {
			got, err := parseDateTime(c.raw)
			if err != nil {
				t.Fatalf("parseDateTime(%q): %v", c.raw, err)
			}
			if got.HasZone != c.hasZone {
				t.Errorf("parseDateTime(%q).HasZone = %v, want %v", c.raw, got.HasZone, c.hasZone)
			}
			if in := got.InZone(blogZone); !in.Equal(c.want) {
				t.Errorf("parseDateTime(%q).InZone = %v, want %v", c.raw, in, c.want)
			}
		}
		if _, err := parseDateTime("not a date"); err == nil {
			t.Errorf("parseDateTime accepted nonsense")
		}
	})

	t.Run("bad packets", func(t *testing.T) {
		for _, bad := range []string{
			"",
			"not xml at all",
			`<?xml version="1.0"?><methodCall><methodName>x</methodName><params><param><value><int>nope</int></value></param></params></methodCall>`,
			`<?xml version="1.0"?><methodCall><params/></methodCall>`,
			`<?xml version="1.0"?><methodResponse><params/></methodResponse>`,
		} {
			if _, err := DecodeCall([]byte(bad)); err == nil {
				t.Errorf("DecodeCall(%.40q) = nil error, want one", bad)
			}
		}
	})

	t.Run("an iso-8859-1 packet is read, not refused", func(t *testing.T) {
		// The as-is declared ISO-8859-1 on its own responses, so clients
		// echo it back on their calls.
		body := []byte(`<?xml version="1.0" encoding="ISO-8859-1"?><methodCall><methodName>metaWeblog.getPost</methodName>` +
			"<params><param><value><string>caf\xe9</string></value></param></params></methodCall>")
		call, err := DecodeCall(body)
		if err != nil {
			t.Fatalf("DecodeCall: %v", err)
		}
		if call.Params[0] != "café" {
			t.Errorf("latin-1 param = %q, want %q", call.Params[0], "café")
		}
	})
}

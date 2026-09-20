package render

import "testing"

// TestGravatarURL pins the avatar URL (PLAN §11, "Gravatar"). The hash
// below is `printf 'myemailaddress@example.com' | md5`, computed outside
// this package so the test pins a known value rather than its own code.
func TestGravatarURL(t *testing.T) {
	const hash = "0bc83cb571cd1c50ba6f3e8a78ef1346" // md5("myemailaddress@example.com")
	const def = "https://blog.example.com/static/images/gravatar.gif"
	want := "https://www.gravatar.com/avatar/" + hash +
		"?s=64&r=pg&d=https%3A%2F%2Fblog.example.com%2Fstatic%2Fimages%2Fgravatar.gif"

	for _, in := range []string{
		"MyEmailAddress@example.com ",
		"myemailaddress@example.com",
		"  MyEmailAddress@example.com\t",
		"MYEMAILADDRESS@EXAMPLE.COM",
	} {
		if got := Gravatar(in, 64, def); got != want {
			t.Errorf("Gravatar(%q) = %q, want %q", in, got, want)
		}
	}

	// The mail templates ask for 80 (PLAN §11).
	if got, want := Gravatar("myemailaddress@example.com", 80, ""),
		"https://www.gravatar.com/avatar/"+hash+"?s=80&r=pg"; got != want {
		t.Errorf("Gravatar(size 80, no default) = %q, want %q", got, want)
	}

	// An empty address still hashes, to md5("").
	if got, want := Gravatar("", 64, ""),
		"https://www.gravatar.com/avatar/d41d8cd98f00b204e9800998ecf8427e?s=64&r=pg"; got != want {
		t.Errorf("Gravatar(empty) = %q, want %q", got, want)
	}
}

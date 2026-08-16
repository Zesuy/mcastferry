package urlx

import "testing"

func TestParse(t *testing.T) {
	target, err := Parse("/udp/239.1.2.3:7980/")
	if err != nil {
		t.Fatal(err)
	}
	if got := target.Group.String(); got != "239.1.2.3" || target.Port != 7980 {
		t.Fatalf("unexpected target %+v", target)
	}
}

func TestParseRejectsNonCanonicalTargets(t *testing.T) {
	for _, path := range []string{
		"/udp/239.1.2.3:7980",
		"/rtp/239.1.2.3:7980/",
		"/udp/10.0.0.1:7980/",
		"/udp/239.1.2.3:0/",
		"/udp/239.1.2.3:65536/",
		"/udp/239.1.2.3:7980/extra/",
		"/udp/239.1.2.3%3A7980/",
		"/udp/10.0.0.1@239.1.2.3:7980/",
	} {
		if _, err := Parse(path); err == nil {
			t.Fatalf("expected %q to be rejected", path)
		}
	}
}

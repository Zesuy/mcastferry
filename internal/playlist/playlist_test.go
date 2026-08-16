package playlist

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadRegularPlaylist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "iptv.m3u8")
	if err := os.WriteFile(path, []byte("#EXTM3U\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := New(Config{Path: path, Route: "/playlist.m3u", MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	file, err := h.Read()
	if err != nil {
		t.Fatal(err)
	}
	if string(file.Data) != "#EXTM3U\n" || file.ContentType != "application/vnd.apple.mpegurl" {
		t.Fatalf("unexpected playlist: %+v", file)
	}
}

func TestRejectsLargeAndSymlinkedPlaylist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "iptv.m3u8")
	if err := os.WriteFile(path, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Path: path, Route: "/playlist.m3u", MaxBytes: 4}); err == nil {
		t.Fatal("expected oversized playlist rejection")
	}
	link := filepath.Join(dir, "linked.m3u8")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{Path: link, Route: "/playlist.m3u", MaxBytes: 1024}); err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestValidateRoute(t *testing.T) {
	for _, route := range []string{"", "/", "relative", "/status", "/udp/list", "/a//b", "/list?x=1"} {
		if err := ValidateRoute(route); err == nil {
			t.Fatalf("expected route %q to be rejected", route)
		}
	}
	if err := ValidateRoute("/playlist.m3u"); err != nil {
		t.Fatal(err)
	}
}

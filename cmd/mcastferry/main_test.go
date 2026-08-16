package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(&stdout, &stderr, []string{"-version"}); code != 0 {
		t.Fatalf("run returned %d: %s", code, stderr.String())
	}
	if got := stdout.String(); got != "mcastferry dev\n" {
		t.Fatalf("unexpected version output %q", got)
	}
}

func TestHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(&stdout, &stderr, []string{"-help"}); code != 2 {
		t.Fatalf("run returned %d", code)
	}
	if !strings.Contains(stderr.String(), "Usage: mcastferry") {
		t.Fatalf("help missing usage: %q", stderr.String())
	}
}

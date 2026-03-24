package version

import (
	"strings"
	"testing"
)

func TestStringDefault(t *testing.T) {
	s := String()
	if !strings.Contains(s, "jumpgate dev") {
		t.Errorf("String() = %q, expected to contain 'jumpgate dev'", s)
	}
	if !strings.Contains(s, "commit: unknown") {
		t.Errorf("String() = %q, expected to contain 'commit: unknown'", s)
	}
}

func TestStringCustom(t *testing.T) {
	origV, origC, origD := Version, Commit, Date
	defer func() { Version, Commit, Date = origV, origC, origD }()

	Version = "1.2.3"
	Commit = "abc1234"
	Date = "2026-01-01T00:00:00Z"

	s := String()
	if !strings.Contains(s, "1.2.3") {
		t.Errorf("String() = %q, missing version", s)
	}
	if !strings.Contains(s, "abc1234") {
		t.Errorf("String() = %q, missing commit", s)
	}
	if !strings.Contains(s, "2026-01-01T00:00:00Z") {
		t.Errorf("String() = %q, missing date", s)
	}
}

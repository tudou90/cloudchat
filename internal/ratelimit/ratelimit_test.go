package ratelimit

import (
	"testing"
	"time"
)

func TestNormalizeIP(t *testing.T) {
	cases := map[string]string{
		"203.0.113.7":              "203.0.113.7",
		"::ffff:203.0.113.7":       "203.0.113.7",
		"2001:db8:abcd:12:1:2:3:4": "2001:db8:abcd:12::/64",
		"2001:db8:abcd:12:ffff::9": "2001:db8:abcd:12::/64", // same /64, same key
		"2001:db8:abcd:13::1":      "2001:db8:abcd:13::/64",
		"not-an-ip":                "unknown",
		"":                         "unknown",
	}
	for in, want := range cases {
		if got := NormalizeIP(in); got != want {
			t.Errorf("NormalizeIP(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTokenBucket(t *testing.T) {
	b := NewTokenBucket(3, 2) // burst 3, 2/s
	now := time.Unix(1000, 0)
	for i := 0; i < 3; i++ {
		if !b.Allow(now) {
			t.Fatalf("burst message %d refused", i+1)
		}
	}
	if b.Allow(now) {
		t.Fatal("4th message in the same instant should be refused")
	}
	if b.Allow(now.Add(400 * time.Millisecond)) {
		t.Fatal("0.4s refills only 0.8 tokens; should still refuse")
	}
	if !b.Allow(now.Add(600 * time.Millisecond)) {
		t.Fatal("after 0.6s a token should be available")
	}
	if !b.Allow(now.Add(time.Hour)) || !b.Allow(now.Add(time.Hour)) || !b.Allow(now.Add(time.Hour)) {
		t.Fatal("bucket should refill to capacity after a long pause")
	}
	if b.Allow(now.Add(time.Hour)) {
		t.Fatal("refill must not exceed capacity")
	}
}

func TestConnCounter(t *testing.T) {
	cc := NewConnCounter(2)
	if !cc.Acquire("a") || !cc.Acquire("a") {
		t.Fatal("first two connections should be allowed")
	}
	if cc.Acquire("a") {
		t.Fatal("third concurrent connection should be refused")
	}
	if !cc.Acquire("b") {
		t.Fatal("other clients are counted separately")
	}
	cc.Release("a")
	if !cc.Acquire("a") {
		t.Fatal("a released slot should be reusable")
	}
}

func TestHumanSeconds(t *testing.T) {
	for in, want := range map[int]string{1: "1 second", 45: "45 seconds", 60: "1 minute", 61: "2 minutes", 3600: "1 hour", 7200: "2 hours"} {
		if got := humanSeconds(in); got != want {
			t.Errorf("humanSeconds(%d) = %q, want %q", in, got, want)
		}
	}
}

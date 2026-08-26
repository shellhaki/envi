package otp

import (
	"context"
	"testing"
	"time"
)

func TestIssueVerifyOnce(t *testing.T) {
	m := NewMemory()
	s := Service{Store: m, TTL: time.Minute}
	code, err := s.Issue(context.Background(), "User@Example.com")
	if err != nil || len(code) != 6 {
		t.Fatal(err)
	}
	if err := s.Verify(context.Background(), " user@example.com ", code); err != nil {
		t.Fatal(err)
	}
	if err := s.Verify(context.Background(), "user@example.com", code); err == nil {
		t.Fatal("OTP reused")
	}
}

func TestExpired(t *testing.T) {
	m := NewMemory()
	s := Service{Store: m, TTL: time.Nanosecond}
	code, _ := s.Issue(context.Background(), "a@b.com")
	time.Sleep(time.Millisecond)
	if err := s.Verify(context.Background(), "a@b.com", code); err == nil {
		t.Fatal("expired OTP accepted")
	}
}

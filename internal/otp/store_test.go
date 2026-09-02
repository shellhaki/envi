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

func TestWrongAttemptDoesNotConsume(t *testing.T) {
	m := NewMemory()
	s := Service{Store: m, TTL: time.Minute, MaxAttempts: 5}
	code, err := s.Issue(context.Background(), "user@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Verify(context.Background(), "user@example.com", "wrong"); err == nil {
		t.Fatal("wrong code accepted")
	}
	// A mistyped attempt must not burn a still-valid code, and surrounding
	// whitespace on the entered code is tolerated.
	if err := s.Verify(context.Background(), "user@example.com", "  "+code+"\n"); err != nil {
		t.Fatalf("correct code rejected after a wrong attempt: %v", err)
	}
}

package httpx

import "testing"

func TestLimiter(t *testing.T) {
	l := Limiter{PerMinute: 3}
	for i := 0; i < 3; i++ {
		if !l.Allow("1.2.3.4") {
			t.Fatalf("attempt %d must pass", i+1)
		}
	}
	if l.Allow("1.2.3.4") {
		t.Error("4th attempt within a minute must be rejected")
	}
	if !l.Allow("5.6.7.8") {
		t.Error("other address is independent")
	}
}

package room

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tinymonster123/live-stream-lab/apps/control-plane/internal/contracts"
)

func TestJoinTokenRoundTrip(t *testing.T) {
	signer := tokenSigner{secret: []byte("test-secret")}
	now := time.Unix(1_700_000_000, 0)
	want := joinClaims{
		RoomCode:      "ABC234",
		MeetingID:     "m1",
		ParticipantID: "p1",
		Role:          contracts.Host,
		ExpiresAt:     now.Add(time.Hour).Unix(),
	}

	raw, err := signer.issue(want)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	got, err := signer.verify(raw, now)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got != want {
		t.Fatalf("claims = %+v, want %+v", got, want)
	}
}

func TestJoinTokenRejectsExpired(t *testing.T) {
	signer := tokenSigner{secret: []byte("test-secret")}
	now := time.Unix(1_700_000_000, 0)

	raw, err := signer.issue(joinClaims{
		RoomCode:  "ABC234",
		Role:      contracts.Guest,
		ExpiresAt: now.Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	if _, err := signer.verify(raw, now.Add(2*time.Minute)); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("err = %v, want ErrExpiredToken", err)
	}
}

func TestJoinTokenRejectsTampering(t *testing.T) {
	signer := tokenSigner{secret: []byte("test-secret")}
	now := time.Unix(1_700_000_000, 0)

	raw, err := signer.issue(joinClaims{
		RoomCode:  "ABC234",
		Role:      contracts.Guest,
		ExpiresAt: now.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	tampered := strings.Replace(raw, "v1.", "v1.e", 1)
	if _, err := signer.verify(tampered, now); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestJoinTokenRejectsWrongSecret(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	raw, err := tokenSigner{secret: []byte("secret-a")}.issue(joinClaims{
		RoomCode:  "ABC234",
		Role:      contracts.Guest,
		ExpiresAt: now.Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	if _, err := (tokenSigner{secret: []byte("secret-b")}).verify(raw, now); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestJoinTokenRejectsMalformed(t *testing.T) {
	signer := tokenSigner{secret: []byte("test-secret")}
	now := time.Unix(1_700_000_000, 0)

	for _, raw := range []string{"", "v1", "v1.payload", "v2.a.b", "v1.@@@.sig"} {
		if _, err := signer.verify(raw, now); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("%q: err = %v, want ErrInvalidToken", raw, err)
		}
	}
}

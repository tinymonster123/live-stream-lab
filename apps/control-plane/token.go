package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const joinTokenVersion = "v1"

var (
	ErrInvalidJoinToken = errors.New("invalid join token")
	ErrExpiredJoinToken = errors.New("join token expired")
)

// joinClaims is the signed payload of a Join Token. A host token has no
// ParticipantID; a participant token is bound to one Participant and Room.
type joinClaims struct {
	RoomCode      string          `json:"room"`
	MeetingID     string          `json:"meeting"`
	ParticipantID string          `json:"pid,omitempty"`
	Role          RosterEntryRole `json:"role"`
	ExpiresAt     int64           `json:"exp"`
}

type tokenSigner struct {
	secret []byte
}

func (signer tokenSigner) issue(claims joinClaims) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	body := joinTokenVersion + "." + base64.RawURLEncoding.EncodeToString(payload)
	signature := signer.mac(body)
	return body + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (signer tokenSigner) verify(raw string, now time.Time) (joinClaims, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || parts[0] != joinTokenVersion {
		return joinClaims{}, ErrInvalidJoinToken
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, signer.mac(parts[0]+"."+parts[1])) {
		return joinClaims{}, ErrInvalidJoinToken
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return joinClaims{}, ErrInvalidJoinToken
	}

	var claims joinClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return joinClaims{}, ErrInvalidJoinToken
	}
	if claims.RoomCode == "" || !claims.Role.Valid() {
		return joinClaims{}, ErrInvalidJoinToken
	}
	if claims.ExpiresAt <= now.Unix() {
		return joinClaims{}, ErrExpiredJoinToken
	}
	return claims, nil
}

func (signer tokenSigner) mac(body string) []byte {
	mac := hmac.New(sha256.New, signer.secret)
	mac.Write([]byte(body))
	return mac.Sum(nil)
}

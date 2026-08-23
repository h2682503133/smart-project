package cachex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

const SchemaVersion = 1

type JSONStore interface {
	GetJSON(context.Context, string, any) (bool, error)
	SetJSON(context.Context, string, any, time.Duration) error
	Version(context.Context, string) (uint64, error)
	BumpVersion(context.Context, string) error
}

func Digest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:12])
}

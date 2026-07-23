package controller

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func planKey(ref pluginv1.ArtifactRef) string {
	raw, _ := json.Marshal(ref)
	digest := sha256.Sum256(raw)
	return "plan/" + hex.EncodeToString(digest[:])
}

func stableOffset(ref pluginv1.ArtifactRef, window time.Duration, salt string) time.Duration {
	if window <= 0 {
		return 0
	}
	digest := sha256.Sum256([]byte(ref.PluginID + "\x00" + ref.Version + "\x00" + ref.Channel + "\x00" + salt))
	return time.Duration(binary.BigEndian.Uint64(digest[:8]) % uint64(window))
}

// scheduledAt only moves scans earlier than the safety lead-time boundary.
func scheduledAt(ref pluginv1.ArtifactRef, expiresAt time.Time, lead, jitter time.Duration) time.Time {
	return expiresAt.Add(-lead).Add(-stableOffset(ref, jitter, "schedule"))
}

func retryAt(ref pluginv1.ArtifactRef, now time.Time, attempts uint32, base, maximum time.Duration) time.Time {
	delay := base
	for i := uint32(1); i < attempts && delay < maximum/2; i++ {
		delay *= 2
	}
	if delay > maximum {
		delay = maximum
	}
	return now.Add(delay + stableOffset(ref, base, "retry"))
}

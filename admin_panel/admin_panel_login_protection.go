package admin_panel

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	adminLoginFailureWindow    = 15 * time.Minute
	adminLoginLockoutThreshold = 5
	adminLoginLockoutDuration  = 15 * time.Minute
	adminLoginBackoffBase      = 2 * time.Second
	adminLoginBackoffMax       = 30 * time.Second
)

type adminLoginAttemptState struct {
	FirstFailureAt      time.Time
	LastFailureAt       time.Time
	ConsecutiveFailures int
	NextAllowedAt       time.Time
	LockedUntil         time.Time
}

var adminLoginGuards = struct {
	sync.Mutex
	byIP   map[string]adminLoginAttemptState
	byUser map[string]adminLoginAttemptState
}{
	byIP:   map[string]adminLoginAttemptState{},
	byUser: map[string]adminLoginAttemptState{},
}

func adminClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if forwarded != "" {
		first := strings.TrimSpace(strings.Split(forwarded, ",")[0])
		if first != "" {
			return first
		}
	}
	host := strings.TrimSpace(r.RemoteAddr)
	if host == "" {
		return ""
	}
	if ip, _, err := net.SplitHostPort(host); err == nil {
		return strings.TrimSpace(ip)
	}
	return host
}

func adminRateLimitUserKey(username string) string {
	key := strings.ToLower(strings.TrimSpace(username))
	if key == "" {
		return "<empty>"
	}
	return key
}

func adminLoginAllowed(clientIP string, userKey string, now time.Time) (bool, time.Duration) {
	adminLoginGuards.Lock()
	defer adminLoginGuards.Unlock()

	waitIP := adminStateWait(adminLoginGuards.byIP, clientIP, now)
	waitUser := adminStateWait(adminLoginGuards.byUser, userKey, now)
	wait := waitIP
	if waitUser > wait {
		wait = waitUser
	}
	if wait > 0 {
		return false, wait
	}
	return true, 0
}

func adminStateWait(bucket map[string]adminLoginAttemptState, key string, now time.Time) time.Duration {
	key = strings.TrimSpace(key)
	if key == "" {
		return 0
	}
	state, ok := bucket[key]
	if !ok {
		return 0
	}
	if now.Sub(state.LastFailureAt) > adminLoginFailureWindow && now.After(state.NextAllowedAt) && now.After(state.LockedUntil) {
		delete(bucket, key)
		return 0
	}
	wait := time.Duration(0)
	if now.Before(state.NextAllowedAt) {
		wait = state.NextAllowedAt.Sub(now)
	}
	if now.Before(state.LockedUntil) {
		lockWait := state.LockedUntil.Sub(now)
		if lockWait > wait {
			wait = lockWait
		}
	}
	return wait
}

func adminRecordLoginFailure(clientIP string, userKey string, now time.Time) {
	adminLoginGuards.Lock()
	defer adminLoginGuards.Unlock()
	adminLoginGuards.byIP[clientIP] = adminNextLoginState(adminLoginGuards.byIP[clientIP], now)
	adminLoginGuards.byUser[userKey] = adminNextLoginState(adminLoginGuards.byUser[userKey], now)
}

func adminNextLoginState(prev adminLoginAttemptState, now time.Time) adminLoginAttemptState {
	state := prev
	if state.FirstFailureAt.IsZero() || now.Sub(state.FirstFailureAt) > adminLoginFailureWindow {
		state = adminLoginAttemptState{
			FirstFailureAt:      now,
			LastFailureAt:       now,
			ConsecutiveFailures: 1,
		}
	} else {
		state.LastFailureAt = now
		state.ConsecutiveFailures++
	}

	if state.ConsecutiveFailures >= adminLoginLockoutThreshold {
		state.LockedUntil = now.Add(adminLoginLockoutDuration)
		state.NextAllowedAt = state.LockedUntil
		return state
	}

	backoff := adminLoginBackoffBase * time.Duration(1<<(state.ConsecutiveFailures-1))
	if backoff > adminLoginBackoffMax {
		backoff = adminLoginBackoffMax
	}
	state.NextAllowedAt = now.Add(backoff)
	return state
}

func adminClearLoginGuards(clientIP string, userKey string) {
	adminLoginGuards.Lock()
	defer adminLoginGuards.Unlock()
	delete(adminLoginGuards.byIP, strings.TrimSpace(clientIP))
	delete(adminLoginGuards.byUser, strings.TrimSpace(userKey))
}

func adminFormatWait(wait time.Duration) string {
	if wait <= 0 {
		return "a moment"
	}
	seconds := int(wait.Round(time.Second).Seconds())
	if seconds < 1 {
		seconds = 1
	}
	return fmt.Sprintf("%ds", seconds)
}

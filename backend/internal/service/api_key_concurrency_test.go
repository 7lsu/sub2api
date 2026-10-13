package service

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type keepSlotRefresh struct {
	owned bool
	err   error
}

// fakeAPIKeySlotCache 按队列依次返回续期结果；队列耗尽后一律视为仍持有。
type fakeAPIKeySlotCache struct {
	mu           sync.Mutex
	refreshes    []keepSlotRefresh
	refreshCalls int
	releases     int
}

func (f *fakeAPIKeySlotCache) TrackAPIKeySlot(context.Context, int64, string) error { return nil }

func (f *fakeAPIKeySlotCache) GetAPIKeyConcurrencyBatch(context.Context, []int64) (map[int64]int, error) {
	return nil, nil
}

func (f *fakeAPIKeySlotCache) AcquireAPIKeySlot(context.Context, int64, int, string) (bool, error) {
	return true, nil
}

func (f *fakeAPIKeySlotCache) ReleaseAPIKeySlot(context.Context, int64, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.releases++
	return nil
}

func (f *fakeAPIKeySlotCache) RefreshAPIKeySlot(context.Context, int64, string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshCalls++
	if len(f.refreshes) == 0 {
		return true, nil
	}
	next := f.refreshes[0]
	f.refreshes = f.refreshes[1:]
	return next.owned, next.err
}

func (f *fakeAPIKeySlotCache) counts() (refreshCalls, releases int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refreshCalls, f.releases
}

const keepSlotTestInterval = 5 * time.Millisecond

// 一次 Redis 抖动不能掐断正在进行的请求：槽位 TTL 远长于续期间隔，下一轮重试即可。
func TestKeepAPIKeySlot_TransientRefreshErrorDoesNotCancel(t *testing.T) {
	cache := &fakeAPIKeySlotCache{refreshes: []keepSlotRefresh{
		{err: errors.New("redis blip")},
		{owned: true},
	}}
	var lost atomic.Int32
	release := keepAPIKeySlot(context.Background(), cache, 1, "req", true, func() { lost.Add(1) }, keepSlotTestInterval)
	defer release()

	require.Eventually(t, func() bool {
		calls, _ := cache.counts()
		return calls >= 3
	}, time.Second, time.Millisecond, "refresh loop must survive a transient error")
	require.Zero(t, lost.Load())
}

func TestKeepAPIKeySlot_LostLeaseCancelsOnlyWhenEnforcing(t *testing.T) {
	t.Run("limited key cancels the request", func(t *testing.T) {
		cache := &fakeAPIKeySlotCache{refreshes: []keepSlotRefresh{{owned: false}}}
		var lost atomic.Int32
		release := keepAPIKeySlot(context.Background(), cache, 1, "req", true, func() { lost.Add(1) }, keepSlotTestInterval)
		defer release()

		require.Eventually(t, func() bool { return lost.Load() == 1 }, time.Second, time.Millisecond)
	})

	t.Run("unlimited key only stops tracking", func(t *testing.T) {
		cache := &fakeAPIKeySlotCache{refreshes: []keepSlotRefresh{{owned: false}}}
		var lost atomic.Int32
		release := keepAPIKeySlot(context.Background(), cache, 1, "req", false, func() { lost.Add(1) }, keepSlotTestInterval)
		defer release()

		require.Eventually(t, func() bool {
			calls, _ := cache.counts()
			return calls == 1
		}, time.Second, time.Millisecond)
		require.Never(t, func() bool {
			calls, _ := cache.counts()
			return calls > 1 || lost.Load() != 0
		}, 50*time.Millisecond, keepSlotTestInterval, "refresh loop must stop without cancelling")
	})
}

func TestKeepAPIKeySlot_ReleaseIsIdempotentAndStopsRefresh(t *testing.T) {
	cache := &fakeAPIKeySlotCache{}
	release := keepAPIKeySlot(context.Background(), cache, 1, "req", true, nil, keepSlotTestInterval)

	release()
	release()

	calls, releases := cache.counts()
	require.Equal(t, 1, releases)
	require.Never(t, func() bool {
		after, _ := cache.counts()
		return after != calls
	}, 50*time.Millisecond, keepSlotTestInterval, "no refresh may run after release")
}

// 客户端断开时由 context.AfterFunc 释放槽位，不依赖调用方记得调用 release。
func TestKeepAPIKeySlot_ContextCancelReleasesSlot(t *testing.T) {
	cache := &fakeAPIKeySlotCache{}
	ctx, cancel := context.WithCancel(context.Background())
	release := keepAPIKeySlot(ctx, cache, 1, "req", true, nil, time.Hour)

	cancel()
	require.Eventually(t, func() bool {
		_, releases := cache.counts()
		return releases == 1
	}, time.Second, time.Millisecond)

	release()
	_, releases := cache.counts()
	require.Equal(t, 1, releases, "explicit release after cancellation must not release twice")
}

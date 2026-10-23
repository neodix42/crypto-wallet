package secureui

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeActivityCallbackIsValueFreeRateLimitedAndCleared(t *testing.T) {
	defer SetActivity(nil)
	var count atomic.Int32
	SetActivity(func() { count.Add(1) })
	for i := 0; i < 10000; i++ {
		walletNativeActivity()
	}
	if count.Load() != 1 {
		t.Fatal("native activity flood reached private pipe", count.Load())
	}
	nativeActivity.Lock()
	nativeActivity.last = time.Now().Add(-2 * time.Second)
	nativeActivity.Unlock()
	walletNativeActivity()
	if count.Load() != 2 {
		t.Fatal("continued input did not refresh activity")
	}
	SetActivity(nil)
	walletNativeActivity()
	if count.Load() != 2 {
		t.Fatal("input outside private request notified coordinator")
	}
	// A new request can report immediately, even after a previous request's key.
	SetActivity(func() { count.Add(1) })
	walletNativeActivity()
	if count.Load() != 3 {
		t.Fatal("previous request throttled current request")
	}
}

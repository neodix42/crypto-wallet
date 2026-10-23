package secureui

import "C"

import (
	"sync"
	"time"
)

var nativeActivity struct {
	sync.Mutex
	callback func()
	last     time.Time
}

// SetActivity installs an input-only notification for the current private
// request. It must be cleared before replying. Notifications contain no input
// values and are limited to one per second; redraws and timers never notify.
func SetActivity(callback func()) {
	nativeActivity.Lock()
	nativeActivity.callback = callback
	nativeActivity.last = time.Time{}
	nativeActivity.Unlock()
}

//export walletNativeActivity
func walletNativeActivity() {
	nativeActivity.Lock()
	f := nativeActivity.callback
	now := time.Now()
	if f == nil || (!nativeActivity.last.IsZero() && now.Sub(nativeActivity.last) < time.Second) {
		nativeActivity.Unlock()
		return
	}
	nativeActivity.last = now
	nativeActivity.Unlock()
	f()
}

package sync

import (
	"sync"
	"time"
)

type UnlockDelayMutex struct {
	sync.Mutex
}

func (m *UnlockDelayMutex) DelayUnlock(delay time.Duration) {
	go func() {
		time.Sleep(delay)

		m.Mutex.Unlock()
	}()
}

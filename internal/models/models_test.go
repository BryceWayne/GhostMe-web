package models

import (
	"sync"
	"testing"
	"time"
)

func TestClientLocking(t *testing.T) {
	client := &Client{
		Email:       "test@example.com",
		DisplayName: "Tester",
	}

	// Basic lock/unlock check
	client.Lock()
	client.Unlock()

	// Concurrency check
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client.Lock()
			time.Sleep(time.Millisecond)
			client.Unlock()
		}()
	}
	wg.Wait()
}

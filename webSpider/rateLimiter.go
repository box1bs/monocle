package webSpider

import (
	"sync"
	"time"
)

type RateLimiter struct {
	token chan struct{}
	quit  chan struct{}
	wg    *sync.WaitGroup
}

func NewRateLimiter(rate int) *RateLimiter {
	rl := &RateLimiter{
		token: make(chan struct{}),
		quit:  make(chan struct{}),
		wg:    new(sync.WaitGroup),
	}
	rl.wg.Add(1)
	go rl.HandleLimits(rate)
	return rl
}

func (rl *RateLimiter) HandleLimits(requestsPerSecond int) {
	defer rl.wg.Done()
	tic := time.NewTicker(time.Duration(1e9 / float64(requestsPerSecond)))
	defer tic.Stop()

	for {
		select {
		case <-rl.quit:
			return

		case <-tic.C:
			select {
			case rl.token <- struct{}{}:
			default:
			}
		}
	}
}

func (rl *RateLimiter) GetToken() {
	<-rl.token
}

func (rl *RateLimiter) Shutdown() {
	close(rl.quit)
	rl.wg.Wait()
	close(rl.token)
}
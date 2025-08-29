package scraper

import (
	"sync"
	"time"
)

type rateLimiter struct {
	token chan struct{}
	quit  chan struct{}
	wg    *sync.WaitGroup
}

func newRateLimiter(rate int) *rateLimiter {
	rl := &rateLimiter{
		token: make(chan struct{}),
		quit:  make(chan struct{}),
		wg:    new(sync.WaitGroup),
	}
	rl.wg.Add(1)
	go rl.handleLimits(rate)
	return rl
}

func (rl *rateLimiter) handleLimits(requestsPerSecond int) {
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

func (rl *rateLimiter) getToken() {
	<-rl.token
}

func (rl *rateLimiter) shutdown() {
	close(rl.quit)
	rl.wg.Wait()
	close(rl.token)
}
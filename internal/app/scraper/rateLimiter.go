package scraper

import (
	"context"
	"sync"
	"time"
)

type rateLimiter struct {
	token 		chan struct{}
	quit  		chan struct{}
	wg    		*sync.WaitGroup
}

func NewRateLimiter(rate int) *rateLimiter {
	rl := &rateLimiter{
		token: make(chan struct{}, 1),
		quit:  make(chan struct{}),
		wg:    new(sync.WaitGroup),
	}
	rl.wg.Add(1)
	go rl.handleLimits(rate)
	return rl
}

func (rl *rateLimiter) handleLimits(requestsPerSecond int) {
	defer rl.wg.Done()
	tic := time.NewTicker(time.Second * time.Duration(requestsPerSecond))
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

func (rl *rateLimiter) GetToken(ctx context.Context) {
	select {
	case <-rl.token:
		return
	case <-rl.quit:
		return
	case <-ctx.Done():
		return
	}
}

func (rl *rateLimiter) Shutdown() {
	close(rl.quit)
	rl.wg.Wait()
	close(rl.token)
}
package workerPool

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/box1bs/monocle/pkg/logger"
)

type WorkerPool struct {
	log 		*logger.Logger
	buf 		chan func()
	quit      	chan struct{}
	wg        	*sync.WaitGroup
	ctx 		context.Context
	workers   	int32
}

func NewWorkerPool(size int, queueCapacity int, c context.Context, l *logger.Logger) *WorkerPool {
	wp := &WorkerPool{
		log:       	l,
		buf: 		make(chan func(), queueCapacity),
		wg:        	new(sync.WaitGroup),
		quit:      	make(chan struct{}),
		ctx: 		c,	
	}
	go func() {
		t := time.NewTicker(30 * time.Second)
		for range t.C {
			select{
			case <-c.Done():
				if len(wp.buf) != 0 {
					continue
				}
				tmp := wp.workers
				<-t.C
				if tmp == wp.workers {
					t.Stop()
					wp.cleanCalls()
				}
			default:
			}
		}
	}()
	for range size {
		go wp.worker()
	}
	return wp
}
	
func (wp *WorkerPool) cleanCalls() {
	for range wp.workers {
		wp.wg.Done()
	}
}

func (wp *WorkerPool) Submit(task func()) {
	wp.wg.Add(1)
	wp.log.Write(logger.NewMessage(logger.WORKER_POOL_LAYER, logger.DEBUG, "Submitting task. Buffer: %d, Workers: %d", len(wp.buf), wp.workers))

	wrap := func() {
		task()
		wp.wg.Done()
	}

	select{
	case wp.buf <- wrap:
	default:
		wrap()
	}
}

func (wp *WorkerPool) worker() {
	atomic.AddInt32(&wp.workers, 1)
	defer atomic.AddInt32(&wp.workers, -1)
	for {
		select {
		case task, ok := <-wp.buf:
			if !ok {
				return
			}
			task()
		case <-wp.quit:
			return
		}
	}
}

func (wp *WorkerPool) Wait() {
	wp.wg.Wait()
}

func (wp *WorkerPool) Stop() {
	close(wp.quit)
	close(wp.buf)
	wp.Wait()
}
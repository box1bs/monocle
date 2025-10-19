package workerPool

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

type WorkerPool struct {
	buf 		chan func()
	quit      	chan struct{}
	wg        	*sync.WaitGroup
	ctx 		context.Context
	workers   	int32
}

func NewWorkerPool(size int, queueCapacity int, c context.Context) *WorkerPool {
	wp := &WorkerPool{
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
	log.Printf("Submitting task. Buffer: %d, Workers: %d", len(wp.buf), wp.workers) //заменить на локальный DEBUG тип

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
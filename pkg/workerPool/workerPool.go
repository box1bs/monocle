package workerPool

import (
	"sync"
	"sync/atomic"
)

type WorkerPool struct {
	tasks     chan func()
	taskQueue chan func()
	wg        *sync.WaitGroup
	quit      chan struct{}
	workers   int32
}

func NewWorkerPool(size int, queueCapacity int) *WorkerPool {
	wp := &WorkerPool{
		tasks:     make(chan func(), size*2),
		taskQueue: make(chan func(), queueCapacity),
		wg:        new(sync.WaitGroup),
		quit:      make(chan struct{}),
	}
	go func() {
		for {
			select {
			case task := <-wp.taskQueue:
				wp.tasks <- task
			case <-wp.quit:
				return
			}
		}
	}()
	for i := 0; i < size; i++ {
		go wp.worker()
	}
	return wp
}

func (wp *WorkerPool) Submit(task func()) {
	wp.wg.Add(1)
	wp.taskQueue <- func() {
		defer wp.wg.Done()
		task()
	}
}

func (wp *WorkerPool) worker() {
	atomic.AddInt32(&wp.workers, 1)
	defer atomic.AddInt32(&wp.workers, -1)
	for {
		select {
		case task, ok := <-wp.tasks:
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
	wp.Wait()
}
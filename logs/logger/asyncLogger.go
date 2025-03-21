package logger

import (
	"io"
	"log"
	"sync"
)

type AsyncLogger struct {
	wr 		io.Writer
	ch   	chan string
	wg   	sync.WaitGroup
}

func NewAsyncLogger(wr io.Writer) (*AsyncLogger, error) {
	al := &AsyncLogger{
		wr: 	wr,
		ch:   	make(chan string, 10000),
	}
	al.wg.Add(1)
	go func() {
		defer al.wg.Done()
		for msg := range al.ch {
			_, err := al.wr.Write([]byte(msg))
			if err != nil {
				log.Println("error logging url: " + msg)
			}
		}
	}()
	return al, nil
}

func (l *AsyncLogger) Write(data string) {
	select {
	case l.ch <- data:
	default:
		l.ch <- data
	}
}

func (l *AsyncLogger) Close() {
	close(l.ch)
	l.wg.Wait()
}

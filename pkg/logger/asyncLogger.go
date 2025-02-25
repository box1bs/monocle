package logger

import (
	"bufio"
	"log"
	"os"
	"sync"
)

type AsyncLogger struct {
	File *os.File
	ch   chan string
	wg   sync.WaitGroup
}

func NewAsyncLogger(fileName string) (*AsyncLogger, error) {
	file, err := os.OpenFile(fileName, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return nil, err
	}
	al := &AsyncLogger{
		File: file,
		ch:   make(chan string, 10000),
	}
	al.wg.Add(1)
	go func() {
		defer al.wg.Done()
		writer := bufio.NewWriter(al.File)
		for msg := range al.ch {
			_, err := writer.WriteString(msg + "\n")
			if err != nil {
				log.Println("error logging url: " + msg)
			}
			writer.Flush()
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

func (l *AsyncLogger) Close() error {
	close(l.ch)
	l.wg.Wait()
	return l.File.Close()
}

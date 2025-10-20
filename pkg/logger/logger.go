package logger

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	INFO = iota
	DEBUG
	ERROR
	CRITICAL_ERROR
)

const (
	INDEX_LAYER = iota
	SCRAPER_LAYER
	REPOSITORY_LAYER
	SEARCHER_LAYER
	MAIN_LAYER
	WORKER_POOL_LAYER
)

type Logger struct {
	ch   	chan message
	wg   	*sync.WaitGroup
}

type message struct {
	text 	string
	t 		int
	layer 	int
}

func NewLogger(info, error io.Writer, cap int) *Logger {
	log := &Logger{
		ch:   	make(chan message, cap),
		wg: 	new(sync.WaitGroup),	
	}
	log.wg.Add(1)
	go func() {
		defer log.wg.Done()
		for msg := range log.ch {
			switch msg.t {
			case INFO, DEBUG:
				if msg.t == DEBUG {
					os.Stdout.Write(log.compareMessage(msg))
					continue
				}
				info.Write(log.compareMessage(msg))
			case ERROR, CRITICAL_ERROR:
				if msg.t == CRITICAL_ERROR {
					os.Stderr.Write(log.compareMessage(msg))
				}
				error.Write(log.compareMessage(msg))
			}
		}
	}()
	return log
}

func (log *Logger) compareMessage(msg message) []byte {
	var s strings.Builder
	s.WriteString(time.Now().Local().Format("2006-01-02 15:04:05"))
	switch msg.t {
	case INFO:
		s.WriteString(" INFO: ")
	case DEBUG:
		s.WriteString(" DEBUG: ")
	case ERROR:
		s.WriteString(" ERROR: ")
	case CRITICAL_ERROR:
		s.WriteString(" CRITICAL_ERROR: ")
	}
	s.WriteString(msg.text + fmt.Sprintf(" on layer: %d ", msg.layer))
	return []byte(s.String())
}

func (log *Logger) Write(msg message) {
	select {
	case log.ch <- msg:
	default:
		fmt.Printf("log channel full, dropping log: %s", msg.text)
	}
}

func (log *Logger) Close() {
	close(log.ch)
	log.wg.Wait()
}

func NewMessage(layer, Type int, format string, v ...any) message {
	return message{
		text: fmt.Sprintf(format, v...),
		layer: layer,
		t: Type,
	}
}
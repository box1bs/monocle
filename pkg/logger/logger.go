package logger

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

type messageType int

const (
	INFO = iota
	DEBUG
	ERROR
	CRITICAL_ERROR
)

type layer int

const (
	MAIN_LAYER = iota
	INDEX_LAYER
	SCRAPER_LAYER
	REPOSITORY_LAYER
	SEARCHER_LAYER
	WORKER_POOL_LAYER
)

type Logger struct {
	ch   	chan message
	wg   	*sync.WaitGroup
}

type message struct {
	text 	string
	t 		messageType
	layer 	layer
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
				info.Write(log.compareMessage(msg))
			case ERROR, CRITICAL_ERROR:
				error.Write(log.compareMessage(msg))
			}
		}
	}()
	return log
}

func (log *Logger) compareMessage(msg message) []byte {
	var s strings.Builder
	s.WriteString(time.Now().Format("2006-01-02 15:04:05"))
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
	s.WriteString(fmt.Sprintf("%s on layer: %d\n",strings.TrimSuffix(msg.text, "\n"), msg.layer))
	return []byte(s.String())
}

func (log *Logger) Write(msg message) {
	select {
	case log.ch <- msg:
	default:
	}
}

func (log *Logger) Close() {
	close(log.ch)
	log.wg.Wait()
}

func NewMessage(layer layer, Type messageType, format string, v ...any) message {
	return message{
		text: fmt.Sprintf(format, v...),
		layer: layer,
		t: Type,
	}
}
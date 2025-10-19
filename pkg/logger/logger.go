package logger

import (
	"fmt"
	"io"
	"log"
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

func NewAsyncLogger(info, error io.Writer) (*Logger, error) {
	al := &Logger{
		ch:   	make(chan message, 10000),
		wg: 	new(sync.WaitGroup),	
	}
	al.wg.Add(1)
	go func() {
		defer al.wg.Done()
		for msg := range al.ch {
			switch msg.t {
			case INFO, DEBUG:
				info.Write([]byte(al.CompareMessage(msg)))
			case ERROR, CRITICAL_ERROR:
				error.Write([]byte(al.CompareMessage(msg)))
			}
		}
	}()
	return al, nil
}

func (l *Logger) CompareMessage(msg message) string {
	var s strings.Builder
	s.WriteString(time.Now().Local().String())
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
	return s.String()
}

func (l *Logger) Write(msg message) {
	select {
	case l.ch <- msg:
	default:
		log.Println("log channel full, dropping log: " + msg.text)
	}
}

func (l *Logger) Close() {
	close(l.ch)
	l.wg.Wait()
}

func NewMessage(text string, layer, t int) message {
	return message{
		text: text,
		layer: layer,
		t: t,
	}
}
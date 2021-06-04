package main

import (
	"fmt"
	"github.com/artvel/display"
	"log"
	"os"
	"os/signal"
	"time"
)

func main() {
	l := display.Find()
	defer func() {
		panicCheck(l.Close())
	}()

	panicCheck(l.Write(0, "First line..."))
	progressExample(l)
	go l.Listen(func(btn int, released bool) bool {
		_ = l.Write(0, fmt.Sprintf("btn:%v released:%v", btn, released))
		return true // true = proceed | false = stop listening
	})

	interruptWaiter = make(chan os.Signal, 3)
	signal.Notify(interruptWaiter, os.Interrupt)

	<-interruptWaiter
	time.Sleep(1 * time.Second)
}

func progressExample(l display.LCD) {
	go func() {
		for i := 0; i < 101; i++ {
			time.Sleep(100 * time.Millisecond)
			err := l.Write(1, display.Progress(i))
			if err != nil {
				log.Println(err)
				return
			}
		}
		interruptWaiter <- &sig{}
	}()
}

var interruptWaiter chan os.Signal

type sig struct{}

func (s *sig) String() string {
	return ""
}
func (s *sig) Signal() {}
func panicCheck(err error) {
	if err != nil {
		panic(err)
	}
}

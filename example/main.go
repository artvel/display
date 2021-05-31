package main

import (
	"github.com/artvel/display"
	"fmt"
	"time"
)

func main() {
	l, err := display.NewQnapLCD("")
	panicCheck(err)
	defer func() {
		panicCheck(l.Close())
	}()
	panicCheck(l.Write(0, "The first line?"))
	panicCheck(l.Write(1, "Hello second line"))
	go l.Listen(func(btn int, released bool) bool {
		//_ = l.Write(0, "button clicked")
		_ = l.Write(1, fmt.Sprintf("id:%d, released:%v", btn, released))
		return true // true = proceed | false = stop listening
	})
	time.Sleep(60 * time.Second)
	panicCheck(l.Enable(false))
	time.Sleep(5 * time.Second)
}

func panicCheck(err error) {
	if err != nil {
		panic(err)
	}
}

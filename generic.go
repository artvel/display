package display

import (
	"errors"
	"log"
	"time"
)

type (
	LCD interface {
		// Listen blocking for button events.
		// Please note, not all devices support released=true.
		Listen(l func(btn int, released bool) bool)
		// Enable(turn on) or disable(turn off) the display.
		Enable(yes bool) error
		// Write a string message on line one or two.
		// If text is longer than supported, it will be cut.
		Write(line Line, text string) error
		// Reopen the instance after a Close call.
		Open() error
		// Close the connection to the display.
		Close() error
	}
	// The line on the display. Most of them support only 0 and 1.
	Line int
	// Placeholder for an actual implementation
	dummy struct{}
)

var (
	DummyLCD             = &dummy{}
	ErrClosed            = errors.New("display closed")
	ErrDisplayNotWorking = errors.New("display not working")
	ErrMsgSizeMismatch   = errors.New("msg size mismatch")
)

const (
	LineOne Line = 0
	LineTwo Line = 1
	// ReadTimeout to break probing
	ReadTimeout               = 300 * time.Millisecond
	DefaultDelayBetweenWrites = 10 * time.Millisecond

	DefaultTTy = "/dev/ttyS1"
)

// Factory function to probe the correct implementation
func FindLED() LCD {
	var (
		lcd LCD
		err error
	)
	lcd, err = NewAsustorLCD("")
	if err == nil {
		return lcd
	} else {
		lcd, err = NewQnapLCD("")
		if err == nil {
			return lcd
		} else {
			log.Println(err)
		}
	}
	return DummyLCD
}

/*
 Dummy functions to use as an actual display.
 As the display is mostly a nice to have feature anyways.
*/
func (d *dummy) Listen(l func(btn int, released bool) bool) {}
func (d *dummy) Enable(yes bool) error                      { return nil }
func (d *dummy) Write(line Line, text string) error         { return nil }
func (d *dummy) Open() error                                { return nil }
func (d *dummy) Close() error                               { return nil }

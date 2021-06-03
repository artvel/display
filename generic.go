package display

import (
	"errors"
	"log"
	"strings"
)

type (
	LCD interface {
		// Reopen the instance after a Close call.
		Open() error
		// Write a string message on line one or two.
		// If text is longer than supported, it will be cut.
		Write(line Line, text string) error
		// Enable(turn on) or disable(turn off) the display.
		Enable(yes bool) error
		// Listen blocking for button events.
		// Please note, not all devices support released=true.
		Listen(l func(btn int, released bool) bool)
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
	LineOne    Line = 0
	LineTwo    Line = 1
	DefaultTTy      = "/dev/ttyS1"
)

// Factory function to probe the correct implementation
func Find() LCD {
	var (
		lcd LCD
		err error
	)
	lcd, err = NewAsustorLCD("")
	if err == nil {
		log.Println("Using Asustor LCD")
		return lcd
	} else {
		lcd, err = NewQnapLCD("")
		if err == nil {
			log.Println("Using Qnap LCD")
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
func (d *dummy) Open() error                                { return nil }
func (d *dummy) Write(line Line, text string) error         { return nil }
func (d *dummy) Enable(yes bool) error                      { return nil }
func (d *dummy) Listen(l func(btn int, released bool) bool) {}
func (d *dummy) Close() error                               { return nil }

func prepareTxt(txt string) string {
	l := len(txt)
	if l > 16 {
		txt = txt[0:16]
	} else if l < 16 {
		txt += strings.Repeat(" ", 16-l)
	}
	return txt
}

func Progress(perc int) string {
	chars := percentOf(16, 100, perc)
	return strings.Repeat("\n", chars) + strings.Repeat("-", 16-chars)
}

func percentOf(maxVal, maxPercent, currentPercent int) int {
	return (maxVal * currentPercent) / maxPercent
}

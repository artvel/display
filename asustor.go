/*
Implements the serial communication protocol for the ASUSTOR
LCD display. This includes controlling and updating and listening for
button presses.

asustorLCD data format:

	MESSAGE_TYPE DATA_LENGTH COMMAND [[DATA]...] [CRC]
*/
package display

import (
	"bytes"
	"errors"
	"github.com/chmorgan/go-serial2/serial"
	"io"
	"log"
	"strings"
	"sync"
	"time"
)

// we hide the struct and its fields
// to keep the usage as simple as possible
// through the LCD interface
type asustorLCD struct {
	con   io.ReadWriteCloser
	readC chan []byte
	btnC  chan []byte
	tty   string
	open  bool

	lastFlush time.Time

	// keep the fields packed inside the struct
	// to simplify the implementation of other
	// displays on the package level
	// and to prevent from reserving memory for
	// unused package fields
	cmdByte          byte
	replyByte        byte
	cmdDisplayStatus []byte
	cmdDisplayOff    []byte
	cmdClearDisplay  []byte
	cmdDisplayOn     []byte
	cmdBtn           []byte
	cmdRdy           []byte
	cmdMsgSentCheck  []byte
}

/**
Supports the display of the following devices:
	Asustor AS6404T
	... add more ..

The constructor is responsible for init and probe.
To simplify and unify the use of future displays.
*/
func NewAsustorLCD(tty string) (LCD, error) {
	if tty == "" {
		tty = DefaultTTy
	}
	cmdByte := byte(0xF0)
	replyByte := byte(0xF1)
	m := &asustorLCD{
		tty:   tty,
		readC: make(chan []byte, 20),
		btnC:  make(chan []byte, 20),

		cmdByte:   cmdByte,
		replyByte: replyByte,

		// TODO(mafredri): Figure out if there are even more commands, and what,
		// if anything, modifying the argument value does.
		// DisplayStatus is used to establish (initial) sync, it is sent
		// repeatedly until the correct response is received. It is also
		// used as an occasional probe that everything is OK?
		//
		// TODO(mafredri): Try to verify if this message is correct
		// and/or if it has a dual purpose.
		cmdDisplayStatus: []byte{cmdByte, 1, 17, 1},
		// DisplayOff turns the display off.
		cmdDisplayOff: []byte{cmdByte, 1, 17, 0},
		// ClearDisplay clears the current text from the display.
		// TODO(ave) no need for this, can't think of a case... remove or implement Clear on interface?
		cmdClearDisplay: []byte{cmdByte, 1, 18, 1},
		// DisplayOn turns the display on.
		// TODO(mafredri): Verify if this is the only purpose?
		cmdDisplayOn: []byte{cmdByte, 1, 34, 0},

		cmdBtn: []byte{cmdByte, 1, 128},
		cmdRdy: []byte{replyByte, 1},
	}
	m.cmdMsgSentCheck = append(m.cmdRdy, 39, 0, 25)

	// initial check if we can connect to a device
	// that works our way
	err := m.Open()
	// return only an error as the display
	// can't be controlled by this implementation
	if err != nil {
		return nil, err
	}
	return m, err
}

func (a *asustorLCD) Open() error {
	if a.open {
		return nil
	}
	var err error
	if a.con != nil {
		_ = a.con.Close()
	}
	a.con, err = serial.Open(serial.OpenOptions{
		PortName:        a.tty,
		BaudRate:        115200,
		DataBits:        8,
		StopBits:        1,
		MinimumReadSize: 5,
		Rs485RxDuringTx: true,
	})
	if err != nil {
		return err
	}
	err = a.flush(a.cmdDisplayStatus)
	if err != nil {
		_ = a.con.Close()
		return err
	}

	// to ensure that we are not stepping into it while
	// the bios is using it, we keep trying a couple of times
	// until the beginning of the read message is equal cmdRdy
	if !a.isReady() {
		err = a.flush(a.cmdClearDisplay)
		if err != nil {
			_ = a.con.Close()
			return err
		}
		if !a.isReady() {
			err = a.flush(a.cmdDisplayStatus)
			if err != nil {
				_ = a.con.Close()
				return err
			}
			if !a.isReady() {
				_ = a.con.Close()
				return ErrDisplayNotWorking
			}
		}
	}

	// if we reach this point, it will most likely
	// work as indented as we received feedback after writing
	a.open = true

	go a.read()

	return nil
}

func (a *asustorLCD) isReady() bool {
	res := make([]byte, 5)
	i, er := a.readWithTimeout(res)
	if er != nil || i != len(res) {
		log.Println("NO", res)
		return false
	}
	if bytes.HasPrefix(res, a.cmdRdy) {
		log.Println("YES", res)
		return true
	}
	log.Println("NO", res)
	return false
}

func (a *asustorLCD) readWithTimeout(res []byte) (i int, err error) {
	respReceived := false
	waiter := sync.WaitGroup{}
	waiter.Add(2)
	time.AfterFunc(ReadTimeout, func() {
		if respReceived {
			return
		}
		_ = a.forceClose()
		err = ErrDisplayNotWorking
		waiter.Done()
	})
	go func() {
		i, err = a.con.Read(res)
		if err == nil {
			respReceived = true
			waiter.Done()
			waiter.Done()
		} else {
			waiter.Done()
		}
	}()
	waiter.Wait()
	return
}

func (a *asustorLCD) Enable(yes bool) error {
	if !a.open {
		return ErrClosed
	}
	if yes {
		return a.flush(a.cmdDisplayOn)
	} else {
		return a.flush(a.cmdDisplayOff)
	}
}

func (a *asustorLCD) Listen(l func(btn int, released bool) bool) {
	if !a.open {
		return
	}
	for a.open {
		res := <-a.btnC
		if !l(int(res[3]), true) {
			return
		}
	}
}

// Write messages to the display. Note that checksum is omitted,
// this is handled by the implementation.
// If text is longer than supported, it will be cut.
func (a *asustorLCD) Write(line Line, text string) error {
	if !a.open {
		return ErrClosed
	}
	err := a.flush(a.strToBytes(line, text))
	if err != nil {
		return err
	}
	if !a.isMsgSent() {
		return ErrDisplayNotWorking
	}
	return err
}

func (a *asustorLCD) isMsgSent() bool {
	if a.open {
		res := <-a.readC
		if bytes.Equal(res, a.cmdMsgSentCheck) {
			log.Println("msg check OK!")
			return true
		}
	}
	log.Println("msg check Not OK!")
	return false
}

func (a *asustorLCD) makemsg(msg []byte) []byte {
	data := make([]byte, len(msg), len(msg)+1)
	copy(data, msg)
	data = append(data, checksum(data))
	return data
}

// read reads asynchronously from the serial port
// and transmits messages on the read or btn channel.
func (a *asustorLCD) read() {
	for a.open {
		res := make([]byte, 5)
		i, er := a.con.Read(res)
		if er != nil {
			return
		}
		log.Println("read", i, er, res)
		if bytes.HasPrefix(res, a.cmdBtn) {
			a.btnC <- res
		} else {
			a.readC <- res
		}
	}
}

// write synchronously to the serial port.
func (a *asustorLCD) flush(data []byte) error {
	data = a.makemsg(data)

	a.wait10MillisForSureBetweenWrites()

	n, err := a.con.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return errors.New("written size does not match")
	}
	return err
}

func (a *asustorLCD) wait10MillisForSureBetweenWrites() {
	timeDiff := a.lastFlush.Add(DefaultDelayBetweenWrites).Sub(time.Now())
	if timeDiff > 0 {
		time.Sleep(timeDiff)
	}
	a.lastFlush = time.Now()
}

func (a *asustorLCD) forceClose() error {
	a.open = false
	if a.con == nil {
		return nil
	}
	return a.con.Close()
}

// Close the serial connection.
func (a *asustorLCD) Close() error {
	if !a.open {
		return nil
	}
	return a.forceClose()
}

func checksum(b []byte) (s byte) {
	for _, bb := range b {
		s += bb
	}
	return s
}

func (a *asustorLCD) strToBytes(line Line, text string) (raw []byte) {
	if len(text) > 16 {
		text = text[0:16]
	}
	if len(text) < 16 {
		text += strings.Repeat(" ", 16-len(text))
	}
	return append([]byte{a.cmdByte, 0x12, 0x27, byte(line), byte(0)}, []byte(text)...)
}

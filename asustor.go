/*
Implements the serial communication protocol for the ASUSTOR
LCD display. This includes controlling and updating and listening for
button presses.

asustor data format:

	MESSAGE_TYPE DATA_LENGTH COMMAND [[DATA]...] [CRC]
*/
package display

import (
	"bytes"
	"errors"
	"github.com/chmorgan/go-serial2/serial"
	"io"
	"strings"
	"sync"
	"time"
)

// we hide the struct and its fields
// to keep the usage as simple as possible
// through the LCD interface
type asustor struct {
	con           io.ReadWriteCloser
	readC         chan []byte
	btnC          chan []byte
	tty           string
	open          bool
	keepListening bool

	// to keep track of the 10ms
	// we have to wait for to be flushed
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
	m := &asustor{
		tty:   tty,
		readC: make(chan []byte, 20),
		btnC:  make(chan []byte, 20),

		cmdByte:   cmdByte,
		replyByte: replyByte,

		cmdDisplayStatus: []byte{cmdByte, 1, 17, 1},
		cmdDisplayOff:    []byte{cmdByte, 1, 17, 0},
		cmdClearDisplay:  []byte{cmdByte, 1, 18, 1},
		cmdDisplayOn:     []byte{cmdByte, 1, 34, 0},

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

func (a *asustor) Open() error {
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

func (a *asustor) isReady() bool {
	res := make([]byte, 5)
	i, er := a.readWithTimeout(res)
	if er != nil || i != len(res) {
		//log.Println("NO", res)
		return false
	}
	if bytes.HasPrefix(res, a.cmdRdy) {
		//log.Println("YES", res)
		return true
	}
	//log.Println("NO", res)
	return false
}

func (a *asustor) readWithTimeout(res []byte) (i int, err error) {
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

func (a *asustor) Enable(yes bool) error {
	if !a.open {
		return ErrClosed
	}
	if yes {
		return a.flush(a.cmdDisplayOn)
	} else {
		return a.flush(a.cmdDisplayOff)
	}
}

func (a *asustor) Listen(l func(btn int, released bool) bool) {
	if !a.open {
		return
	}
	a.keepListening = true
	for a.open {
		res := <-a.btnC
		if !a.open {
			return
		}
		if a.keepListening {
			if !l(int(res[3]), true) {
				a.keepListening = false
				return
			}
		}
	}
}

// Write messages to the display. Note that checksum is omitted,
// this is handled by the implementation.
// If text is longer than supported, it will be cut.
func (a *asustor) Write(line Line, text string) error {
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

func (a *asustor) isMsgSent() bool {
	res := <-a.readC
	if !a.open {
		return false
	}
	if bytes.Equal(res, a.cmdMsgSentCheck) {
		//log.Println("msg check OK!")
		return true
	}
	//log.Println("msg check Not OK!")
	return false
}

func (a *asustor) makemsg(msg []byte) []byte {
	data := make([]byte, len(msg), len(msg)+1)
	copy(data, msg)
	data = append(data, checksum(data))
	return data
}

// read reads asynchronously from the serial port
// and transmits messages on the read or btn channel.
func (a *asustor) read() {
	for a.open {
		res := make([]byte, 5)
		_, er := a.con.Read(res)
		if er != nil || !a.open {
			return
		}
		//log.Println("read", i, er, res)
		if bytes.HasPrefix(res, a.cmdBtn) {
			if a.keepListening {
				a.btnC <- res
			}
		} else {
			a.readC <- res
		}
	}
}

// write synchronously to the serial port.
func (a *asustor) flush(data []byte) error {
	data = a.makemsg(data)

	a.waitForFlushBetweenWrites()

	n, err := a.con.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return errors.New("written size does not match")
	}
	return err
}

func (a *asustor) waitForFlushBetweenWrites() {
	timeDiff := a.lastFlush.Add(DefaultDelayBetweenWrites).Sub(time.Now())
	if timeDiff > 0 {
		time.Sleep(timeDiff)
	}
	a.lastFlush = time.Now()
}

func (a *asustor) forceClose() error {
	a.open = false
	a.readC <- []byte{}
	a.btnC <- []byte{}
	if a.con == nil {
		return nil
	}
	return a.con.Close()
}

// Close the serial connection.
func (a *asustor) Close() error {
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

func (a *asustor) strToBytes(line Line, text string) (raw []byte) {
	if len(text) > 16 {
		text = text[0:16]
	}
	if len(text) < 16 {
		text += strings.Repeat(" ", 16-len(text))
	}
	return append([]byte{a.cmdByte, 0x12, 0x27, byte(line), byte(0)}, []byte(text)...)
}

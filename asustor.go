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
	"log"
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

	m sync.Mutex

	retry byte

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

	cmdOkayCheck []byte

	replyOkayCheck1   []byte
	replyOkayCheck2   []byte
	replyOkayCheck3   []byte
	replyMsgSentCheck []byte

	msgSize uint
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
	cmdByte := byte(240)
	replyByte := byte(241)
	m := &asustor{
		tty:   tty,
		readC: make(chan []byte, 100),
		btnC:  make(chan []byte, 100),

		cmdByte:   cmdByte,
		replyByte: replyByte,

		cmdDisplayStatus: []byte{cmdByte, 1, 17, 1},
		cmdDisplayOff:    []byte{cmdByte, 1, 17, 0},
		cmdClearDisplay:  []byte{cmdByte, 1, 18, 1},
		cmdDisplayOn:     []byte{cmdByte, 1, 34, 0},
		cmdBtn:           []byte{cmdByte, 1, 128},

		replyOkayCheck1:   []byte{replyByte, 1, 17, 0, 3},
		replyOkayCheck2:   []byte{replyByte, 1, 17, 4, 7},
		replyOkayCheck3:   []byte{replyByte, 1, 39, 4, 29},
		replyMsgSentCheck: []byte{replyByte, 1, 39, 0, 25},

		msgSize: 5,
	}

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
	a.m.Lock()
	defer a.m.Unlock()

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
		MinimumReadSize: 1,
	})
	if err != nil {
		log.Println(err)
		return err
	}

	a.open = true
	go a.read()
	return a.establish()
}

func (a *asustor) establish() error {
	err := a.flush(a.cmdDisplayStatus)
	if err != nil {
		_ = a.con.Close()
		_ = a.forceClose()
		return err
	}
	if !a.responseEqual(a.replyOkayCheck1, a.replyOkayCheck2, a.replyOkayCheck3) {
		_ = a.con.Close()
		_ = a.forceClose()
		return ErrDisplayNotWorking
	}
	return nil
}

// Write messages to the display. Note that checksum is omitted,
// this is handled by the implementation.
// If text is longer than supported, it will be cut.
func (a *asustor) Write(line Line, text string) error {
	a.m.Lock()
	defer a.m.Unlock()

	return a.write(a.strToBytes(line, text))
}

func (a *asustor) Enable(yes bool) error {
	a.m.Lock()
	defer a.m.Unlock()

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

func (a *asustor) write(msg []byte) error {
	if !a.open {
		return ErrClosed
	}
	err := a.flush(msg)
	if err != nil {
		return err
	}
	if !a.responseEqual(a.replyMsgSentCheck) {
		if a.retry > 10 {
			return ErrDisplayNotWorking
		} else {
			a.retry++
			//log.Println("try", a.retry)
			return a.write(msg)
		}
	} else {
		a.retry = 0
	}
	return err
}

func (a *asustor) responseEqual(checks ...[]byte) bool {
	ch := make(chan bool, 1)
	go func() {
		select {
		case res := <-a.readC:
			if !a.open {
				ch <- false
				return
			}
			for _, check := range checks {
				if bytes.Equal(res, check) {
					//log.Println("msg check OK!")
					ch <- true
					return
				}
			}
			ch <- false
		case <-time.After(40 * time.Millisecond):
			ch <- false
		}
	}()
	return <-ch
}

// read reads asynchronously from the serial port
// and transmits messages on the read or btn channel.
func (a *asustor) read() {
	buf := bytes.Buffer{}
	startFound := false
	res := make([]byte, a.msgSize)
	for a.open {
		i, er := a.con.Read(res)
		if er != nil || !a.open {
			return
		}
		for c := 0; c < i; c++ {
			if startFound || res[c] == a.replyByte || res[c] == a.cmdByte {
				startFound = true
				buf.WriteByte(res[c])
				if buf.Len() == 5 {
					startFound = false
					a.pass(buf.Bytes())
					buf.Reset()
				}
			}
		}
	}
}

func (a *asustor) pass(res []byte) {
	//log.Println("read", res)
	if bytes.HasPrefix(res, a.cmdBtn) {
		a.btnC <- res
	} else {
		a.readC <- res
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

func (a *asustor) makemsg(msg []byte) []byte {
	data := make([]byte, len(msg), len(msg)+1)
	copy(data, msg)
	data = append(data, checksum(data))
	return data
}

func (a *asustor) waitForFlushBetweenWrites() {
	timeDiff := a.lastFlush.Add(10 * time.Millisecond).Sub(time.Now())
	if timeDiff > 0 {
		time.Sleep(timeDiff)
	}
	a.lastFlush = time.Now()
}

func checksum(b []byte) (s byte) {
	for _, bb := range b {
		s += bb
	}
	return s
}

func (a *asustor) strToBytes(line Line, text string) []byte {
	return a.createMsg(line, []byte(prepareTxt(text)))
}

func (a *asustor) createMsg(line Line, text []byte) []byte {
	return append([]byte{a.cmdByte, 0x12, 0x27, byte(line), byte(0)}, text...)
}

// Close the serial connection.
func (a *asustor) Close() error {
	a.m.Lock()
	defer a.m.Unlock()

	if !a.open {
		return nil
	}
	return a.forceClose()
}

func (a *asustor) forceClose() error {
	a.open = false
	a.readC <- []byte{}
	a.btnC <- []byte{}
	return a.con.Close()
}

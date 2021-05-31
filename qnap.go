package display

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"github.com/chmorgan/go-serial2/serial"
	"io"
	"log"
	"strings"
	"sync"
	"time"
)

type qnap struct {
	tty  string
	con  io.ReadWriteCloser
	open bool

	lastFlush time.Time
	writeC    chan []byte
	btnC      chan []byte

	released    []byte
	upPressed   []byte
	downPressed []byte
	bothPressed []byte

	cmdBtn     []byte
	cmdEnable  []byte
	cmdDisable []byte
	cmdWrite   []byte
	cmdInit    []byte
	cmdRdy     []byte
}

/**
Supports the display of the following devices:
	QNAP TVS-x72XT
	... add more ..

The constructor is responsible for init and probe.
To simplify and unify the use of future displays.
*/
func NewQnapLCD(tty string) (LCD, error) {
	if tty == "" {
		tty = DefaultTTy
	}
	cmdBtn := []byte{83, 5, 0}
	q := &qnap{
		tty:         tty,
		released:    append(cmdBtn, 0),
		upPressed:   append(cmdBtn, 1),
		downPressed: append(cmdBtn, 2),
		bothPressed: append(cmdBtn, 3),

		cmdBtn:     cmdBtn,
		cmdEnable:  []byte{77, 94, 1, 10},
		cmdDisable: []byte{77, 94, 0, 10},
		cmdWrite:   []byte{77, 94, 1},
		cmdInit:    []byte{77, 0},
		cmdRdy:     []byte{83, 1, 0, 125},
	}
	err := q.init()
	if err != nil {
		return nil, err
	}
	return q, err
}

func (q *qnap) Open() error {
	if q.open {
		return nil
	}
	return q.init()
}

func (q *qnap) init() error {
	var err error
	q.con, err = serial.Open(serial.OpenOptions{
		PortName:        q.tty,
		BaudRate:        1200,
		DataBits:        8,
		StopBits:        1,
		MinimumReadSize: 4,
		Rs485RxDuringTx: true,
	})
	if err != nil {
		return err
	}
	defer func() {
		if r := recover(); r != nil {
			log.Println("display panic when trying to init")
		}
	}()
	_, err = q.con.Write(q.cmdInit)
	if err != nil {
		_ = q.con.Close()
		return err
	}
	i := 0
	res := make([]byte, 4)
	i, err = q.readWithTimeout(res)
	if err != nil {
		_ = q.con.Close()
		return ErrDisplayNotWorking
	}
	if bytes.Equal(res[0:i], q.cmdRdy) {
		q.open = true
		return nil
	} else {
		q.open = false
		_ = q.con.Close()
		return ErrDisplayNotWorking
	}
}

func (q *qnap) Enable(yes bool) error {
	if !q.open {
		return ErrClosed
	}
	if yes {
		_, err := q.con.Write(q.cmdEnable)
		return err
	} else {
		_, err := q.con.Write(q.cmdDisable)
		return err
	}
}

func (q *qnap) Write(line Line, txt string) error {
	if !q.open {
		return ErrClosed
	}
	if len(txt) > 16 {
		//cut if longer
		txt = txt[:16]
	} else {
		//fill rest empty
		for len(txt) < 16 {
			txt += strings.Repeat(" ", 16-len(txt))
		}
	}
	//TODO improve this line
	cnt := append(q.cmdWrite, []byte(fmt.Sprintf("%s%s", h(fmt.Sprintf("4d0c0%d10", line)), txt))...)

	q.wait10MillisForSureBetweenWrites()

	n, err := q.con.Write(cnt)

	if n != len(cnt) {
		return ErrMsgSizeMismatch
	}
	return err
}

func (q *qnap) wait10MillisForSureBetweenWrites() {
	timeDiff := q.lastFlush.Add(DefaultDelayBetweenWrites).Sub(time.Now())
	if timeDiff > 0 {
		time.Sleep(timeDiff)
	}
	q.lastFlush = time.Now()
}

func (q *qnap) Listen(l func(btn int, released bool) bool) {
	if !q.open {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.Println("display panic while listening")
		}
	}()
	var lastBtn = 0
	for {
		res := make([]byte, 4)
		n, err := q.con.Read(res)
		if err != nil {
			return
		}
		if n != len(res) {
			continue
		}
		res = q.ensureOrder(res)
		if bytes.Equal(res, q.released) {
			if !l(lastBtn, true) {
				return
			}
			lastBtn = 0
		} else if bytes.Equal(res, q.upPressed) {
			if lastBtn == 3 {
				continue
			}
			lastBtn = 1
			if !l(lastBtn, false) {
				return
			}
		} else if bytes.Equal(res, q.downPressed) {
			if lastBtn == 3 {
				continue
			}
			lastBtn = 2
			if !l(lastBtn, false) {
				return
			}
		} else if bytes.Equal(res, q.bothPressed) {
			lastBtn = 3
			if !l(lastBtn, false) {
				return
			}
		}
	}
}

func (q *qnap) ensureOrder(res []byte) []byte {
	if bytes.HasPrefix(res, q.cmdBtn) {
		return res
	}
	// happens only if read and write cross to much
	ordered := make([]byte, 4)
	for i, b := range q.cmdBtn {
		for c, r := range res {
			if b == r {
				ordered[i] = b
				res = remove(res, c)
				break
			}
		}
	}
	if len(res) > 0 {
		ordered[3] = res[0]
	}
	return ordered
}

func remove(s []byte, i int) []byte {
	s[len(s)-1], s[i] = s[i], s[len(s)-1]
	return s[:len(s)-1]
}

func (q *qnap) readWithTimeout(res []byte) (i int, err error) {
	respReceived := false
	waiter := sync.WaitGroup{}
	waiter.Add(2)
	go func() {
		i, err = q.con.Read(res)
		if err == nil {
			respReceived = true
			waiter.Done()
			waiter.Done()
		} else {
			waiter.Done()
		}
	}()
	time.AfterFunc(ReadTimeout, func() {
		if respReceived {
			return
		}
		_ = q.forceClose()
		err = ErrDisplayNotWorking
		waiter.Done()
	})
	waiter.Wait()
	return
}

func h(s string) []byte {
	decoded, _ := hex.DecodeString(s)
	return decoded
}

func (q *qnap) Close() error {
	if !q.open {
		return nil
	}
	return q.forceClose()
}

func (q *qnap) forceClose() error {
	q.open = false
	if q.con == nil {
		return nil
	}
	return q.con.Close()
}

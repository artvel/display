## Mini Display controller
Currently supported displays:
| Completion   |      Device      | 
|----------|:-------------:|
| 100% |  Qnap TVS-x72XT |
| 100% |  Asustor AS6404T | 

Other Qnap or Asustor should be compatible too I think.

### Example usage:
```Go
package main

import (
	"github.com/artvel/display"
	"fmt"
	"time"
)

func main() {
	l, err := display.NewQnapLCD("")
        // l, err := display.NewAsustorLCD("")
	panicCheck(err)
	panicCheck(l.Write(0, "The first line?"))
	panicCheck(l.Write(1, "Hello second line"))
	go l.Listen(func(btn int, released bool) bool {
		_ = l.Write(0, "button clicked")
		_ = l.Write(1, fmt.Sprintf("id:%d, released:%v", btn, released))
		return true
	})
	time.Sleep(20 * time.Second)
	panicCheck(l.Enable(false))
	time.Sleep(5 * time.Second)
	panicCheck(l.Close())
}

func panicCheck(err error){
	if err != nil {
		panic(err)
	}
}
```

### Todo
- cleanup code
- impl. sync to prevent read and write from cutting across
- add more implementation of other displays

PR's are welcome!
Use it as you please.

Have fun.

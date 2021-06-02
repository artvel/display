## Mini Display controller
Currently supported displays:
| Completion   |      Device      | 
|----------|:-------------:|
| 100% |  Qnap TVS-x72XT |
| 100% |  Asustor AS6404T | 

![Qnap](res/qnap.jpg?raw=true "Qnap")
----------------------------------------------
![Asustor](res/asustor.jpg?raw=true "Asustor")

Other Qnap or Asustor devices should be compatible too I think.

### Example usage:
```Go
package main

import (
	"github.com/artvel/display"
	"fmt"
	"time"
)

func main() {
	l := display.Find()
    defer func() {
        panicCheck(l.Close())
    }()
	panicCheck(l.Write(0, "The first line?"))
	panicCheck(l.Write(1, "Hello second line"))
	go l.Listen(func(btn int, released bool) bool {
		_ = l.Write(0, "button clicked")
		_ = l.Write(1, fmt.Sprintf("id:%d, released:%v", btn, released))
		return true
	})
	time.Sleep(20 * time.Second) //wait 20sec for testing the button events
	panicCheck(l.Enable(false)) //disable.. turn of the display
	time.Sleep(5 * time.Second)
}

func panicCheck(err error){
	if err != nil {
		panic(err)
	}
}
```

### Todo
- add more implementation of other displays

PR's are welcome!
Use it as you please.

Have fun.

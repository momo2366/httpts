package main

import (
	"fmt"
	"github.com/godbus/dbus"
	"os"
)

func main() {
	conn, err := dbus.SessionBus()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to connect to session bus:", err)
		os.Exit(1)
	}

	conn.Object("com.kylin.SelfService", "/com/kylin/SelfService")
	// conn.BusObject().Call("com.kylin.SelfService", 0,
		// "type='signal',path='/com/kylin/SelfService',interface='com.kylin.SelfService',sender='com.kylin.SelfService'")

	c := make(chan *dbus.Signal, 10)
	conn.Signal(c)
	for v := range c {
		fmt.Println(v)
	}
}

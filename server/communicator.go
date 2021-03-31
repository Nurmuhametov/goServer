package server

import (
	"fmt"
	"net"
)

type Communicator struct {
	conn                  net.Conn
	dataReceivedListeners []func(str string)
	active                bool
}

func (c Communicator) Start() {
	c.active = true
	go c.communicate()
}

func (c Communicator) communicate() {
	defer c.conn.Close()
	for c.active {
		input := make([]byte, 1024*4)
		n, err := c.conn.Read(input)
		if n == 0 || err != nil {
			fmt.Println("Read error:", err)
			c.active = false
			break
		}
		source := string(input[0:n])
		for _, h := range c.dataReceivedListeners {
			h(source)
		}
	}
}

func (c Communicator) SendData(str string) {
	if c.active {
		n, err := c.conn.Write([]byte(str))
		if n == 0 || err != nil {
			fmt.Println("Write error:", err)
			c.active = false
		}
	}
}

func (c Communicator) Stop() {
	c.active = false
	err := c.conn.Close()
	if err != nil {
		fmt.Println("Write error:", err)
	}
}

func (c Communicator) AddListener(f func(str string)) {
	c.dataReceivedListeners = append(c.dataReceivedListeners, f)
}

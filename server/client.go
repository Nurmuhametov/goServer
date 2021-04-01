package server

import (
	"fmt"
	"net"
)

type ConnectedClient struct {
	conn                  net.Conn
	name                  string
	Lobby                 *Lobby
	dataReceivedListeners []func(str string, client *ConnectedClient)
	active                bool
}

func (c ConnectedClient) StartCommunicator() {
	c.active = true
	go c.communicate()
}

func (c ConnectedClient) communicate() {
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
			h(source, &c)
		}
	}
}

func (c ConnectedClient) SendData(data []byte) {
	if c.active {
		n, err := c.conn.Write(data)
		if n == 0 || err != nil {
			fmt.Println("Write error:", err)
			c.active = false
		}
	}
}

func (c ConnectedClient) Stop() {
	c.active = false
	err := c.conn.Close()
	if err != nil {
		fmt.Println("Closing error:", err)
	}
}

func (c ConnectedClient) AddListener(f func(str string, client *ConnectedClient)) {
	c.dataReceivedListeners = append(c.dataReceivedListeners, f)
}

func (c ConnectedClient) login(string2 string) {

}
func (c ConnectedClient) joinLobby(string2 string) {

}

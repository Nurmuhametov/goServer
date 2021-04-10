package server

import (
	"bufio"
	"fmt"
	"net"
	"sync"
)

//Структура подключенного клиента
type connectedClient struct {
	conn                  net.Conn   //Соединение, по которому осуществляется общение с клиентом
	name                  string     //Логин клиента, аналогичен записи в БД
	dataReceivedListeners *FuncStack //Стек функций, вызываемых при получении сообщения
	active                bool       //Идёт ли общение с данным клиентом
	readMutex             sync.Mutex //мьютекс, который приостанавливает чтение из потока входящих сообщений
}

//Запускает общение с клиентом, начиная прослушивать от него сообщения
func (c *connectedClient) StartCommunicator() {
	c.active = true
	go c.communicate()
}

//Основная функция, которая получает сообщения и вызывает функции из стека
func (c *connectedClient) communicate() {
	defer c.conn.Close()
	for c.active {
		input := make([]byte, 1024*4)
		n, err := c.conn.Read(input)
		c.readMutex.Lock()
		if n == 0 || err != nil {
			fmt.Println("Read error:", err)
			c.active = false
			c.Stop()
			break
		}
		source := string(input[0:n])
		var current = c.dataReceivedListeners
		for {
			funcPeek(current)(source, c)
			if current.next != nil {
				current = current.next
			} else {
				break
			}
		}
		c.readMutex.Unlock()
	}
}

//Отправляет данные клиенту
func (c *connectedClient) SendData(data []byte) {
	//fmt.Printf("Sending %s to %s\n", data, c.name)
	if c.active {
		w := bufio.NewWriter(c.conn)
		if _, err := w.Write(data); err != nil {
			fmt.Println("Write error:", err)
			c.active = false
		}
		_ = w.Flush()
		//n, err := c.conn.Write(data)
		//if n == 0 || err != nil {
		//	fmt.Println("Write error:", err)
		//	c.active = false
		//}
	}
}

//Останавливает общение с клиентом
func (c *connectedClient) Stop() {
	println("Stopping communication")
	c.active = false
	err := c.conn.Close()
	if err != nil {
		fmt.Println("Closing error:", err)
	}
}

//Добавляет в стек функцию для обработки входящих сообщений
func (c *connectedClient) AddListener(f func(str string, client *connectedClient)) {
	funcPush(&c.dataReceivedListeners, f)
}

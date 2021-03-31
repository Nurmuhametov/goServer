package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"net"
	"os"
	"strconv"
)

const MaxPlayers = 24

type Server struct {
	Listener        net.Listener
	connectedClient []ConnectedClient
	lobbies         []Lobby
	db              *sql.DB
	//stmt driver.Stmt
	port   uint
	active bool
}

type configs struct {
	LoginFromFile bool   `json:"loginFromFile"`
	ServerPort    uint   `json:"serverPort"`
	MariaAddress  string `json:"mariaAddress"`
	MariaPort     uint   `json:"mariaPort"`
	DbName        string `json:"dbName"`
	DbLogin       string `json:"dbLogin"`
	DbPassword    string `json:"dbPassword"`
}

func Init() *Server {
	var res = new(Server)
	conf := readConfigs()
	res.Listener, _ = net.Listen("tcp4", ":"+strconv.Itoa(int(conf.ServerPort)))
	var credits = fmt.Sprintf("%s:%s@/%s", conf.DbLogin, conf.DbPassword, conf.DbName)
	db, err := sql.Open("mysql", credits)
	if err != nil {
		panic(err)
	}
	res.db = db
	res.port = conf.ServerPort
	res.active = true
	res.connectedClient = make([]ConnectedClient, 0, MaxPlayers)
	res.lobbies = make([]Lobby, 90)
	return res
}

func (s Server) Start() {
	for s.active {
		_, _ = s.Listener.Accept()
	}
}

func readConfigs() configs {
	file, err := os.Open("resources/config.json")
	if err != nil {
		fmt.Println(err)
		os.Exit(2)
	}

	defer file.Close()

	data := make([]byte, 256)
	_, err = file.Read(data)
	data = bytes.Trim(data, "\x00")
	var res configs
	err = json.Unmarshal(data, &res)
	if err != nil {
		panic(err)
	}
	return res
}

package server

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"math"
	"math/rand"
	"net"
	"os"
	"regexp"
	"strconv"
)

const MaxPlayers = 24

type Server struct {
	Listener        net.Listener
	connectedClient []*ConnectedClient
	lobbies         map[uint]LobbyInfo
	db              *sql.DB
	competitors     []string
	schedule        map[string][]string
	port            uint
	active          bool
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
	res.connectedClient = make([]*ConnectedClient, 0, MaxPlayers)
	res.lobbies = make(map[uint]LobbyInfo, 256)
	return res
}

func (s *Server) Start() {
	println("Server started")
	go func() {
		for s.active {
			var str string
			_, _ = fmt.Scanf("%s\n", &str)
			if str == "exit" {
				s.active = false
				_ = s.Listener.Close()
			}
		}
	}()
	s.updateUsers()
	s.createSchedule()
	s.createGameResults()
	s.createLobbies()
	s.updateLobbies()
	for s.active {
		println("Waiting for connection")
		conn, err := s.Listener.Accept()
		if err != nil {
			_, _ = os.Stderr.Write([]byte(err.Error()))
			continue
		}
		s.addNewClient(conn)
		fmt.Printf("User [%s] connected\n", conn.RemoteAddr().String())
	}
}

func (s *Server) addNewClient(conn net.Conn) {
	var cc = new(ConnectedClient)
	*cc = ConnectedClient{
		conn:                  conn,
		name:                  "",
		Lobby:                 nil,
		dataReceivedListeners: make([]func(string2 string, client *ConnectedClient), 0, 2),
		active:                false,
	}
	cc.dataReceivedListeners = append(cc.dataReceivedListeners, s.dataReceived)
	cc.StartCommunicator()
	s.connectedClient = append(s.connectedClient, cc)
}

func (s *Server) dataReceived(str string, c *ConnectedClient) {
	re := regexp.MustCompile("[A-Z ]+[A-Z]|(?:{.+})")
	split := re.FindAllString(str, 2)
	if len(split) > 0 {
		println(str)
		switch split[0] {
		case "CONNECTION":
			var err error
			c.name, err = s.login(split[1])
			if err != nil {
				msg := Message{Msg: "LOGIN FAILED"}
				data, _ := json.Marshal(msg)
				c.SendData(string(data))
			} else {
				msg := Message{Msg: "LOGIN OK"}
				data, _ := json.Marshal(msg)
				c.SendData(string(data))
			}
		case "SOCKET JOINLOBBY":
			if c.name == "" {
				msg := Message{Msg: "LOGIN FIRST"}
				data, _ := json.Marshal(msg)
				c.SendData(string(data))
			} else {
				res, err := s.joinLobby(split[1], c.name)
				if err != nil {
					println(err)
					var jLR = JoinLobbyResponse{
						Data:    LobbyInfo{},
						Success: false,
					}
					data, _ := json.Marshal(jLR)
					c.SendData(string(data))
				} else {
					c.SendData(res)
				}
			}
		}
	}
}

func (s *Server) login(str string) (string, error) {
	var loginInfo LoginInfo
	err := json.Unmarshal([]byte(str), &loginInfo)
	if err != nil {
		panic(err)
	}
	rows, err2 := s.db.Query("SELECT * FROM user WHERE login = ?", loginInfo.Login)
	if err2 != nil {
		panic(err2)
	}
	if rows.Next() {
		return loginInfo.Login, nil
	} else {
		return "", errors.New("login failed")
	}
}

func (s *Server) updateUsers() {
	_, err := s.db.Exec("CREATE TABLE IF NOT EXISTS user ( `ID` INT UNSIGNED NOT NULL AUTO_INCREMENT , `login` VARCHAR(20) NOT NULL , PRIMARY KEY (`ID`), UNIQUE `login` (`login`)) ENGINE = InnoDB;")
	if err != nil {
		println(err)
	}
	var users, err2 = os.Open("resources/participants_list")
	if err2 != nil {
		println(err)
	}
	var reader = bufio.NewReader(users)
	var listUsers = make([]string, 0, MaxPlayers)
	for {
		user, _, err3 := reader.ReadLine()
		if err3 != nil {
			break
		}
		listUsers = append(listUsers, string(user))
	}
	s.competitors = make([]string, 0, len(listUsers))
	s.competitors = append(s.competitors, listUsers...)
	for _, user := range listUsers {
		_, err = s.db.Exec("INSERT INTO user VALUES (null ,?) ON DUPLICATE KEY UPDATE `login` = ?", user, user)
		if err != nil {
			println(err)
		}
	}
}

func (s *Server) createSchedule() {
	s.schedule = make(map[string][]string, len(s.competitors))
	for _, val := range s.competitors {
		s.schedule[val] = make([]string, 0, (len(s.competitors)-1)*GamesToPlay)
	}
	for i := 0; i < len(s.competitors)-1; i++ {
		for j := 0; j < len(s.competitors); j++ {
			for k := 0; k < GamesToPlay; k++ {
				s.schedule[s.competitors[j]] = append(s.schedule[s.competitors[j]], s.competitors[len(s.competitors)-j-1])
			}
		}
		var tmp = s.competitors[1]
		for n := 1; n < len(s.competitors)-1; n++ {
			s.competitors[n] = s.competitors[n+1]
		}
		s.competitors[len(s.competitors)-1] = tmp
	}
}

func (s *Server) createGameResults() {
	_, err := s.db.Exec("SELECT * FROM game_results")
	if err != nil {
		_, _ = s.db.Exec("CREATE TABLE game_results ( `first` VARCHAR(20) NOT NULL , `second` VARCHAR(20) NOT NULL , `result` SET('first','second','draw') NOT NULL, CONSTRAINT `first` FOREIGN KEY (first) REFERENCES user(login) ON DELETE RESTRICT ON UPDATE RESTRICT, CONSTRAINT `second` FOREIGN KEY (second) REFERENCES user(login) ON DELETE RESTRICT ON UPDATE RESTRICT) ENGINE = InnoDB;")
	}
}

func (s *Server) createLobbies() {
	_, _ = s.db.Exec("CREATE TABLE IF NOT EXISTS lobbies ( `ID` INT UNSIGNED NOT NULL AUTO_INCREMENT , `width` INT UNSIGNED NOT NULL , `height` INT UNSIGNED NOT NULL , `gameBarrierCount` INT UNSIGNED NOT NULL , `playerBarrierCount` INT UNSIGNED NOT NULL , `name` VARCHAR(100) NOT NULL , `playersCount` INT UNSIGNED NOT NULL , PRIMARY KEY (`ID`), UNIQUE `name` (`name`)) ENGINE = InnoDB;")
	for i := 0; i < len(s.competitors)-1; i++ {
		for j := i + 1; j < len(s.competitors); j++ {
			for k := 0; k < GamesToPlay; k++ {
				var width = rand.Uint32()%5 + 5
				var height = rand.Uint32()%5 + 5
				var gameBarriersCount = rand.Uint32()%3 + uint32(math.Log(float64(width+height)/2.0)/math.Log(3))
				var playersBarrierCount = rand.Uint32()%3 + 1
				_, _ = s.db.Exec("INSERT INTO lobbies VALUES (?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE `name` = ?", nil, width, height, gameBarriersCount, playersBarrierCount, fmt.Sprintf("%s_vs_%s_%d", s.competitors[i], s.competitors[j], k+1), 2, fmt.Sprintf("%s_vs_%s_%d", s.competitors[i], s.competitors[j], k+1))
			}
		}
	}
}

func (s *Server) updateLobbies() {
	rows, err := s.db.Query("SELECT * FROM lobbies")
	if err != nil {
		println(err)
		return
	}
	for rows.Next() {
		var id uint
		var width, height, gameBarrierCount, playerBarrierCount, playersCount uint8
		var name string
		err := rows.Scan(&id, &width, &height, &gameBarrierCount, &playerBarrierCount, &name, &playersCount)
		if err != nil {
			println(err)
		}
		var ID = new(string)
		*ID = strconv.Itoa(int(id))
		var lobbyInfo = LobbyInfo{
			ID:                 ID,
			Width:              width,
			Height:             height,
			GameBarrierCount:   gameBarrierCount,
			PlayerBarrierCount: playerBarrierCount,
			Name:               name,
			PlayersCount:       playersCount,
		}
		s.lobbies[id] = lobbyInfo
	}
	fmt.Printf("Server has %d lobbies at the time\n", len(s.lobbies))
}

func (s *Server) joinLobby(str, name string) (string, error) {
	var lobbyID LobbyID
	err := json.Unmarshal([]byte(str), &lobbyID)
	if err != nil {
		return "", err
	}
	if lobbyID.ID != nil {
		var i, _ = strconv.Atoi(*lobbyID.ID)
		rows, err := s.db.Query("SELECT * FROM lobbies WHERE ID = ?", i)
		if err != nil {
			println(err)
		}
		if rows.Next() {
			var joinLobbyResponse JoinLobbyResponse
			var id uint
			var width, height, gameBarrierCount, playerBarrierCount, playersCount uint8
			var name string
			_ = rows.Scan(&id, &width, &height, &gameBarrierCount, &playerBarrierCount, &name, &playersCount)
			var ID = new(string)
			*ID = strconv.Itoa(int(id))
			var lobbyInfo = LobbyInfo{
				ID:                 ID,
				Width:              width,
				Height:             height,
				GameBarrierCount:   gameBarrierCount,
				PlayerBarrierCount: playerBarrierCount,
				Name:               name,
				PlayersCount:       playersCount,
			}
			joinLobbyResponse = JoinLobbyResponse{
				Data:    lobbyInfo,
				Success: true,
			}
			var res, _ = json.Marshal(joinLobbyResponse)
			return string(res), nil
		}
	} else {
		var opponent = s.schedule[name][0]
		rows, err := s.db.Query("SELECT * FROM lobbies WHERE name LIKE %?% AND name LIKE %?%", name, opponent)
		if err != nil {
			return "", err
		}
		if rows.Next() {
			var joinLobbyResponse JoinLobbyResponse
			var id uint
			var width, height, gameBarrierCount, playerBarrierCount, playersCount uint8
			var name string
			_ = rows.Scan(&id, &width, &height, &gameBarrierCount, &playerBarrierCount, &name, &playersCount)
			var ID = new(string)
			*ID = strconv.Itoa(int(id))
			var lobbyInfo = LobbyInfo{
				ID:                 ID,
				Width:              width,
				Height:             height,
				GameBarrierCount:   gameBarrierCount,
				PlayerBarrierCount: playerBarrierCount,
				Name:               name,
				PlayersCount:       playersCount,
			}
			joinLobbyResponse = JoinLobbyResponse{
				Data:    lobbyInfo,
				Success: true,
			}
			var res, _ = json.Marshal(joinLobbyResponse)
			return string(res), nil
		}
	}
	return "", errors.New("not found")
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

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
	"sync"
)

//Максимальное количество подключенных клиентов
const MaxPlayers = 24

//Количество игр, которые клиенты должны сыграть между собой
const GamesToPlay = 5

//Структура, отвечающая за сервер. Не создавать больше одного
type Server struct {
	listener        net.Listener
	connectedClient map[*connectedClient]*Lobby
	lobbies         map[uint]*Lobby
	db              *sql.DB
	competitors     []string
	schedule        map[string][]string
	scheduleMutex   sync.Mutex
	port            uint
	active          bool
}

//Структура с настройками сервера
type configs struct {
	//Булевское значение, если true, то значения берутся из файла, иначе - из консоли
	LoginFromFile bool `json:"loginFromFile"`
	//Порт, который будет прослушиваться
	ServerPort uint `json:"serverPort"`
	//Адрес, по которому нужно обращаться с MariaDB
	MariaAddress string `json:"mariaAddress"`
	//Порт, который слушает MariaDB
	MariaPort uint `json:"mariaPort"`
	//Название базы данных
	DbName string `json:"dbName"`
	//Логин пользователя MariaDB
	DbLogin string `json:"dbLogin"`
	//Логин пользователя MariaDB
	DbPassword string `json:"dbPassword"`
}

//Создаёт экземпляр сервера
func Init() *Server {
	var res = new(Server)
	conf := readConfigs()
	res.listener, _ = net.Listen("tcp4", ":"+strconv.Itoa(int(conf.ServerPort)))
	var credits = fmt.Sprintf("%s:%s@/%s", conf.DbLogin, conf.DbPassword, conf.DbName)
	db, err := sql.Open("mysql", credits)
	if err != nil {
		panic(err)
	}
	res.db = db
	res.port = conf.ServerPort
	res.active = true
	res.connectedClient = make(map[*connectedClient]*Lobby, MaxPlayers)
	res.lobbies = make(map[uint]*Lobby, 256)
	return res
}

//Запускает сервер, который запускает инициализацию всех необходимых таблиц в БД, создаёт расписание матчей
//и начинает принимать входящие подключения
func (s *Server) Start() {
	println("Server started")
	go func() {
		for s.active {
			var str string
			_, _ = fmt.Scanf("%s\n", &str)
			switch str {
			case "exit":
				s.active = false
				_ = s.listener.Close()
			case "stats":
				res := s.getStats()
				for i, val := range res {
					fmt.Printf("%d. %s \t %d\n", i+1, val.Name, val.Points)
				}
			case "delete results":
				_, _ = s.db.Exec("DELETE FROM game_results")
			case "update users":
				s.updateUsers()
			case "delete users":
				_, _ = s.db.Exec("DELETE FROM user")
			case "create schedule":
				s.createSchedule()
			case "delete lobbies":
				_, _ = s.db.Exec("DELETE FROM lobbies")
			case "create lobbies":
				s.createLobbies()
			case "restart":
				fmt.Print("Вы точно ходите перезапустить сервер? Никто в данный момент не должен играть [Y/n]")
				_, _ = fmt.Scanf("%s\n", &str)
				if str == "Y" || str == "y" {
					fmt.Print("Удаляю результаты игр...")
					_, _ = s.db.Exec("DELETE FROM game_results")
					fmt.Print("Удаляю список пользователей...")
					_, _ = s.db.Exec("DELETE FROM user")
					fmt.Print("Удаляю лобби...")
					_, _ = s.db.Exec("DELETE FROM lobbies")
					s.updateUsers()
					s.createSchedule()
					s.createGameResults()
					s.createLobbies()
					s.updateLobbies()
					s.createStats()
					fmt.Print("Server started")
				}
			default:
				fmt.Print("Неизвестная команда. Доступные команды: exit, stats, delete results, update users, delete users, create schedule, delete lobbies, restart\n")
			}
		}
	}()
	s.updateUsers()
	s.createSchedule()
	s.createGameResults()
	s.createLobbies()
	s.updateLobbies()
	s.createStats()
	for s.active {
		println("Waiting for connection")
		conn, err := s.listener.Accept()
		if err != nil {
			_, _ = os.Stderr.Write([]byte(err.Error()))
			continue
		}
		s.addNewClient(conn)
		fmt.Printf("User [%s] connected\n", conn.RemoteAddr().String())
	}
}

//Добавляет нового клиента и устанавливает ему пустое лобби
func (s *Server) addNewClient(conn net.Conn) {
	var cc = new(connectedClient)
	*cc = connectedClient{
		conn:                  conn,
		name:                  "",
		Lobby:                 nil,
		dataReceivedListeners: nil,
		active:                false,
	}

	cc.AddListener(s.dataReceived)
	cc.StartCommunicator()
	s.connectedClient[cc] = nil
}

//Главная функция, обрабатывающая входящие команды
func (s *Server) dataReceived(str string, c *connectedClient) {
	re := regexp.MustCompile("[A-Z ]+[A-Z]|(?:{.+})")
	split := re.FindAllString(str, 2)
	if len(split) > 0 {
		switch split[0] {
		case "CONNECTION":
			var err error
			c.name, err = s.login(split[1])
			if err != nil {
				msg := Message{Msg: "LOGIN FAILED"}
				data, _ := json.Marshal(msg)
				c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
			} else {
				msg := Message{Msg: "LOGIN OK"}
				data, _ := json.Marshal(msg)
				c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
			}
		case "SOCKET JOINLOBBY":
			if c.name == "" {
				msg := Message{Msg: "LOGIN FIRST"}
				data, _ := json.Marshal(msg)
				c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
			} else {
				res, err := s.joinLobby(split[1], c.name)
				if err != nil {
					println(err.Error())
					var jLR = JoinLobbyResponse{
						Data:    LobbyInfo{},
						Success: false,
					}
					data, _ := json.Marshal(jLR)
					c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
					s.connectedClient[c] = nil
					c.Lobby = nil
				} else {
					data, _ := json.Marshal(res)
					i, _ := strconv.Atoi(*res.Data.ID)
					var errCh = make(chan error)
					go s.lobbies[uint(i)].AddPLayer(c, errCh)
					var err2 = <-errCh
					if err2 != nil {
						println(err2.Error())
						var jLR = JoinLobbyResponse{
							Data:    LobbyInfo{},
							Success: false,
						}
						data, _ := json.Marshal(jLR)
						c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
						s.connectedClient[c] = nil
						c.Lobby = nil
					} else {
						s.connectedClient[c] = s.lobbies[uint(i)]
						c.Lobby = s.lobbies[uint(i)]
						c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
						errCh <- nil
					}
				}
			}
		case "DISCONNECT":
			if c.Lobby != nil {
				c.Lobby.removePlayer(c)
			}
			msg := Message{Msg: "BYE"}
			data, _ := json.Marshal(msg)
			c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
			c.Stop()
			var id = func() string {
				if c.name == "" {
					return c.conn.RemoteAddr().String()
				} else {
					return c.name
				}
			}()
			fmt.Printf("User %s disconnected\n", id)
			delete(s.connectedClient, c)
		case "GET LOBBY":
			s.updateLobbies()
			var infos = make([]LobbyInfo, len(s.lobbies), len(s.lobbies))
			for index, lobby := range s.lobbies {
				infos[index] = lobby.Info
			}
			var getLobbyResponse = GetLobbyResponse{
				Data:    infos,
				Success: true,
			}
			data, _ := json.Marshal(getLobbyResponse)
			c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
		case "GET RANDOMLOBBY":
			var lobbyID = LobbyID{}
			var data, _ = json.Marshal(lobbyID)
			c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
		case "POST LOBBY":
			id, err := s.postLobby(split[1])
			if err != nil {
				msg := Message{Msg: "Неправильно ты, дядя Фёдор, лобби постишь, надо по правилам создавать, читай man"}
				data, _ := json.Marshal(msg)
				c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
			} else {
				var lobbyID = LobbyID{ID: &id}
				data, _ := json.Marshal(lobbyID)
				c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
			}
		case "SOCKET LEAVELOBBY":
			c.Lobby = nil
			s.connectedClient[c] = nil
			msg := Message{Msg: "OK"}
			data, _ := json.Marshal(msg)
			c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
		case "GET STATS":
			stats := s.getStats()
			data, _ := json.Marshal(stats)
			c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
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

//Обновляет список пользователей, который берется из файла /resources/participants_list
func (s *Server) updateUsers() {
	_, err := s.db.Exec("CREATE TABLE IF NOT EXISTS user ( `ID` INT UNSIGNED NOT NULL AUTO_INCREMENT , `login` VARCHAR(20) NOT NULL , PRIMARY KEY (`ID`), UNIQUE `login` (`login`)) ENGINE = InnoDB;")
	if err != nil {
		println(err.Error())
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

//Создаёт расписание матчей по круговой системе. Обязательно чётное количество участников
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

//Создаёт (если не существует) таблицу в БД с результатами матчей и добавляет внешние ключи
func (s *Server) createGameResults() {
	_, err := s.db.Exec("SELECT * FROM game_results")
	if err != nil {
		_, _ = s.db.Exec("CREATE TABLE game_results ( `first` VARCHAR(20) NOT NULL , `second` VARCHAR(20) NOT NULL , `result` SET('first','second','draw') NOT NULL, CONSTRAINT `first` FOREIGN KEY (first) REFERENCES user(login) ON DELETE RESTRICT ON UPDATE RESTRICT, CONSTRAINT `second` FOREIGN KEY (second) REFERENCES user(login) ON DELETE RESTRICT ON UPDATE RESTRICT) ENGINE = InnoDB;")
	}
}

//Создаёт таблицу lobbies, если не существует и заполняет её данными из расписания матчей
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

func (s *Server) createStats() {
	_, _ = s.db.Exec("create view if not exists stats as " +
		"select login, sum(Points) as pts from " +
		"(select user.login, Count(*)*3 as Points from user inner join game_results on user.login=game_results.first where result='first' group by ID " +
		"union all " +
		"select user.login, Count(*)*3 from user inner join game_results on user.login=game_results.second where result='second' group by ID " +
		"union all " +
		"select user.login, Count(*) from user inner join game_results on (user.login=game_results.first or user.login=game_results.second) where result='draw' group by ID" +
		") as temporary group by login;")
}

//Обновляет список лобби на сервере из данных в БД
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
		var ID = strconv.Itoa(int(id))
		if err != nil {
			println(err)
		}
		var lobbyInfo = LobbyInfo{
			ID:                 &ID,
			Width:              width,
			Height:             height,
			GameBarrierCount:   gameBarrierCount,
			PlayerBarrierCount: playerBarrierCount,
			Name:               name,
			PlayersCount:       playersCount,
		}
		if _, ok := s.lobbies[id]; !ok {
			s.lobbies[id] = GetLobby(lobbyInfo)
			s.lobbies[id].server = s
		}
	}
	fmt.Printf("Server has %d lobbies at the time\n", len(s.lobbies))
}

//Пытается найти подходящее лобби для игрока с именем name. str - {"id":string}
func (s *Server) joinLobby(str, name string) (JoinLobbyResponse, error) {
	var lobbyID LobbyID
	err := json.Unmarshal([]byte(str), &lobbyID)
	if err != nil {
		return JoinLobbyResponse{}, err
	}
	if lobbyID.ID != nil {
		var i, _ = strconv.Atoi(*lobbyID.ID)
		rows, err := s.db.Query("SELECT * FROM lobbies WHERE ID = ?", i)
		if err != nil {
			return JoinLobbyResponse{}, err
		}
		if rows.Next() {
			var joinLobbyResponse JoinLobbyResponse
			var id uint
			var width, height, gameBarrierCount, playerBarrierCount, playersCount uint8
			var n string
			_ = rows.Scan(&id, &width, &height, &gameBarrierCount, &playerBarrierCount, &n, &playersCount)
			var ID = strconv.Itoa(int(id))
			var lobbyInfo = LobbyInfo{
				ID:                 &ID,
				Width:              width,
				Height:             height,
				GameBarrierCount:   gameBarrierCount,
				PlayerBarrierCount: playerBarrierCount,
				Name:               n,
				PlayersCount:       playersCount,
			}
			joinLobbyResponse = JoinLobbyResponse{
				Data:    lobbyInfo,
				Success: true,
			}
			return joinLobbyResponse, nil
		} else {
			return JoinLobbyResponse{}, errors.New("lobby with current id don't found")
		}
	} else {
		s.scheduleMutex.Lock()
		var opponent = s.schedule[name][0]
		s.schedule[name] = s.schedule[name][1:]
		s.scheduleMutex.Unlock()
		rows, err := s.db.Query("SELECT * FROM lobbies WHERE `name` LIKE concat('%', ?, '%') AND `name` LIKE concat('%', ?, '%')", name, opponent)
		if err != nil {
			return JoinLobbyResponse{}, err
		}
		if rows.Next() {
			var joinLobbyResponse JoinLobbyResponse
			var id uint
			var width, height, gameBarrierCount, playerBarrierCount, playersCount uint8
			var name string
			_ = rows.Scan(&id, &width, &height, &gameBarrierCount, &playerBarrierCount, &name, &playersCount)
			var ID = strconv.Itoa(int(id))
			var lobbyInfo = LobbyInfo{
				ID:                 &ID,
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
			return joinLobbyResponse, nil
		} else {
			return JoinLobbyResponse{}, errors.New("probably player played all his games, can't find lobby with his name")
		}
	}
}

//Создаёт лобби
func (s *Server) postLobby(str string) (string, error) {
	var lobbyInfo LobbyInfo
	err := json.Unmarshal([]byte(str), &lobbyInfo)
	if err != nil {
		return "", err
	}
	res, err2 := s.db.Exec("INSERT INTO lobbies VALUES (?, ?, ?, ?, ?, ?, ?)", nil, lobbyInfo.Width, lobbyInfo.Height, lobbyInfo.GameBarrierCount, lobbyInfo.PlayerBarrierCount, lobbyInfo.Name, lobbyInfo.PlayersCount)
	if err2 != nil {
		return "", err2
	}
	id, err3 := res.LastInsertId()
	if err3 != nil {
		return "", err3
	}
	return strconv.Itoa(int(id)), nil
}

//Отправляет результаты и удалет лобби
func (s *Server) deleteLobby(res result, lobby *Lobby, client, client2 *connectedClient) {
	_, _ = s.db.Exec("INSERT INTO game_results VALUES (? ,?, ?)", res.first, res.second, res.result)
	client.Lobby = nil
	client2.Lobby = nil
	s.connectedClient[client] = nil
	s.connectedClient[client2] = nil
	var id, _ = strconv.Atoi(*lobby.Info.ID)
	delete(s.lobbies, uint(id))
	_, _ = s.db.Exec("DELETE FROM lobbies WHERE ID = ?", id)
	client.readMutex.Unlock()
	client2.readMutex.Unlock()
}

//Возвращает таблицу с текущими результатами
func (s *Server) getStats() []Stats {
	rows, _ := s.db.Query("SELECT * FROM stats")
	var stats = make([]Stats, 0, MaxPlayers)
	for rows.Next() {
		var login string
		var pts uint16
		_ = rows.Scan(&login, &pts)
		stats = append(stats, Stats{
			Name:   login,
			Points: pts,
		})
	}
	return stats[:]
}

//Читает настройки
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

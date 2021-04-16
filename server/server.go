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
	"time"
)

//Максимальное количество подключенных клиентов
const MaxPlayers = 24

var Server = initServer()

//Структура, отвечающая за сервер. Не создавать больше одного
type server struct {
	listener        net.Listener
	connectedClient map[*connectedClient]*Lobby
	clientsMapMutex sync.Mutex
	playingLobbies  map[uint]*Lobby
	lobbiesMutex    sync.Mutex
	db              *sql.DB
	competitors     []string
	schedule        map[string][]string
	scheduleMutex   sync.Mutex
	port            uint
	active          bool
	gamesToPlay     uint
	Configs         configs
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
	//Количество игр в каждой партии
	GamesToPlay uint `json:"gamesToPlay"`
	//Таймаут хода
	Timeout time.Duration `json:"timeout"`
	//Максимальное количество ходов в игре, после чего будет объясвлена ничья
	MaxTurns int `json:"max_turns"`
}

//Создаёт экземпляр сервера
func initServer() *server {
	var res = new(server)
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
	res.playingLobbies = make(map[uint]*Lobby)
	res.gamesToPlay = conf.GamesToPlay
	res.Configs = conf
	return res
}

//Запускает сервер, который запускает инициализацию всех необходимых таблиц в БД, создаёт расписание матчей
//и начинает принимать входящие подключения
func (s *server) Start() {
	fmt.Println("Server started!")
	go s.commandsHandler()
	s.updateUsers()
	s.createSchedule()
	s.createGameResults()
	s.createLobbies()
	s.createStats()
	for s.active {
		fmt.Println("Waiting for connection")
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
func (s *server) addNewClient(conn net.Conn) {
	var cc = new(connectedClient)
	*cc = connectedClient{
		conn:                  conn,
		name:                  "",
		dataReceivedListeners: nil,
		active:                false,
	}
	cc.AddListener(s.dataReceived)
	cc.StartCommunicator()
	s.clientsMapMutex.Lock()
	s.connectedClient[cc] = nil
	s.clientsMapMutex.Unlock()
}

func (s *server) commandsHandler() {
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
				//TODO()
				fmt.Print("server started\n")
			}
		default:
			fmt.Print("Неизвестная команда. Доступные команды: exit, stats, delete results, update users, delete users, create schedule, delete lobbies, restart\n")
		}
	}
}

//Главная функция, обрабатывающая входящие команды
func (s *server) dataReceived(str string, c *connectedClient) {
	re := regexp.MustCompile("[A-Z ]+[A-Z]|(?:{.+})")
	split := re.FindAllString(str, 2)
	if len(split) > 0 {
		switch split[0] {
		case "CONNECTION":
			if len(split) == 2 {
				s.tryLogin(c, split[1])
			}
		case "SOCKET JOINLOBBY":
			if c.name == "" {
				msg := Message{Msg: "LOGIN FIRST"}
				data, _ := json.Marshal(msg)
				c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
			} else {
				if len(split) == 2 {
					s.tryJoinLobby(c, split[1])
				}

			}
		case "DISCONNECT":
			s.disconnect(c)
		case "GET LOBBY":
			s.getLobbies(c)
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
			s.clientsMapMutex.Lock()
			if lobby, ok := s.connectedClient[c]; ok && !lobby.isPlaying {
				lobby.removePlayer(c)
				s.connectedClient[c] = nil
				msg := Message{Msg: "OK"}
				data, _ := json.Marshal(msg)
				c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
			}
			s.clientsMapMutex.Unlock()
		case "GET STATS":
			stats := s.getStats()
			data, _ := json.Marshal(stats)
			c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
		}
	}
}

func (s *server) login(str string) (string, error) {
	var loginInfo LoginInfo
	err := json.Unmarshal([]byte(str), &loginInfo)
	if err != nil {
		return "", err
	}
	rows, err2 := s.db.Query("SELECT * FROM user WHERE login = ?", loginInfo.Login)
	if rows != nil {
		defer rows.Close()
	}
	if err2 != nil {
		return "", err2
	}
	if rows.Next() {
		return loginInfo.Login, nil
	} else {
		return "", errors.New("login failed")
	}

}

//Обновляет список пользователей, который берется из файла /resources/participants_list
func (s *server) updateUsers() {
	_, err := s.db.Exec("CREATE TABLE IF NOT EXISTS user ( `ID` INT UNSIGNED NOT NULL AUTO_INCREMENT , `login` VARCHAR(20) NOT NULL , PRIMARY KEY (`ID`), UNIQUE `login` (`login`)) ENGINE = InnoDB;")
	if err != nil {
		logError(236, err.Error())
	}
	var users, err2 = os.Open("resources/participants_list")
	if err2 != nil {
		logError(240, err2.Error())
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
			logError(256, err.Error())
		}
	}
}

//Создаёт расписание матчей по круговой системе. Обязательно чётное количество участников
func (s *server) createSchedule() {
	s.schedule = make(map[string][]string, len(s.competitors))
	for _, val := range s.competitors {
		s.schedule[val] = make([]string, 0, uint(len(s.competitors)-1)*s.gamesToPlay)
	}
	for i := 0; i < len(s.competitors)-1; i++ {
		for j := 0; j < len(s.competitors); j++ {
			for k := uint(0); k < s.gamesToPlay; k++ {
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
func (s *server) createGameResults() {
	_, err := s.db.Exec("SELECT * FROM game_results")
	if err != nil {
		_, _ = s.db.Exec("CREATE TABLE game_results ( `first` VARCHAR(20) NOT NULL , `second` VARCHAR(20) NOT NULL , `result` SET('first','second','draw') NOT NULL, CONSTRAINT `first` FOREIGN KEY (first) REFERENCES user(login) ON DELETE RESTRICT ON UPDATE RESTRICT, CONSTRAINT `second` FOREIGN KEY (second) REFERENCES user(login) ON DELETE RESTRICT ON UPDATE RESTRICT) ENGINE = InnoDB;")
	}
}

//Создаёт таблицу lobbies, если не существует, и заполняет её данными из расписания матчей
func (s *server) createLobbies() {
	_, _ = s.db.Exec("CREATE TABLE IF NOT EXISTS lobbies ( `ID` INT UNSIGNED NOT NULL AUTO_INCREMENT , `width` INT UNSIGNED NOT NULL , `height` INT UNSIGNED NOT NULL , `gameBarrierCount` INT UNSIGNED NOT NULL , `playerBarrierCount` INT UNSIGNED NOT NULL , `name` VARCHAR(100) NOT NULL , `playersCount` INT UNSIGNED NOT NULL , PRIMARY KEY (`ID`), UNIQUE `name` (`name`)) ENGINE = InnoDB;")
	for i := 0; i < len(s.competitors)-1; i++ {
		for j := i + 1; j < len(s.competitors); j++ {
			for k := uint(0); k < s.gamesToPlay; k++ {
				var width = rand.Uint32()%5 + 5
				var height = rand.Uint32()%5 + 5
				var gameBarriersCount = rand.Uint32()%3 + uint32(math.Log(float64(width+height)/2.0)/math.Log(3))
				var playersBarrierCount = rand.Uint32()%3 + 1
				_, _ = s.db.Exec("INSERT INTO lobbies VALUES (?, ?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE `name` = ?", nil, width, height, gameBarriersCount, playersBarrierCount, fmt.Sprintf("%s_vs_%s_%d", s.competitors[i], s.competitors[j], k+1), 2, fmt.Sprintf("%s_vs_%s_%d", s.competitors[i], s.competitors[j], k+1))
			}
		}
	}
}

func (s *server) createStats() {
	_, _ = s.db.Exec("create view if not exists stats as " +
		"select login, sum(Points) as pts from " +
		"(select user.login, Count(*)*3 as Points from user inner join game_results on user.login=game_results.first where result='first' group by ID " +
		"union all " +
		"select user.login, Count(*)*3 from user inner join game_results on user.login=game_results.second where result='second' group by ID " +
		"union all " +
		"select user.login, Count(*) from user inner join game_results on (user.login=game_results.first or user.login=game_results.second) where result='draw' group by ID" +
		") as temporary group by login;")
}

//Пытается найти подходящее лобби для игрока с именем name. str - {"id":string}
func (s *server) joinLobby(str, name string) (JoinLobbyResponse, error) {
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
		defer rows.Close()
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
		defer rows.Close()
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
func (s *server) postLobby(str string) (string, error) {
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

//Отправляет результаты в БД и удалет лобби
func (s *server) deleteLobby(res result, lobby *Lobby, client, client2 *connectedClient) {
	_, err := s.db.Exec("INSERT INTO game_results VALUES (? ,?, ?)", res.first, res.second, res.result)
	if err != nil {
		logError(444, err.Error())
	}
	s.clientsMapMutex.Lock()
	s.connectedClient[client] = nil
	s.connectedClient[client2] = nil
	s.clientsMapMutex.Unlock()
	var id, _ = strconv.Atoi(*lobby.Info.ID)
	s.lobbiesMutex.Lock()
	delete(s.playingLobbies, uint(id))
	s.lobbiesMutex.Unlock()
	_, _ = s.db.Exec("DELETE FROM lobbies WHERE ID = ?", id)
	client.readMutex.Unlock()
	client2.readMutex.Unlock()
}

//Возвращает таблицу с текущими результатами
func (s *server) getStats() []Stats {
	rows, _ := s.db.Query("SELECT * FROM stats ORDER BY pts DESC")
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

func (s *server) tryLogin(c *connectedClient, str string) {
	var err error
	c.name, err = s.login(str)
	if err != nil {
		msg := Message{Msg: "LOGIN FAILED"}
		data, _ := json.Marshal(msg)
		c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
	} else {
		msg := Message{Msg: "LOGIN OK"}
		data, _ := json.Marshal(msg)
		c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
	}
}

func (s *server) tryJoinLobby(c *connectedClient, str string) {
	res, err := s.joinLobby(str, c.name)
	if err != nil {
		logError(492, err.Error())
		jLR := JoinLobbyResponse{
			Data:    LobbyInfo{},
			Success: false,
		}
		data, _ := json.Marshal(jLR)
		c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
		s.clientsMapMutex.Lock()
		s.connectedClient[c] = nil
		s.clientsMapMutex.Unlock()
	} else {
		data, _ := json.Marshal(res)
		i, _ := strconv.Atoi(*res.Data.ID)
		s.lobbiesMutex.Lock()
		if lobby, ok := s.playingLobbies[uint(i)]; !ok || lobby.expectingPlayer == nil {
			//JoinLobby, но никто ещё не подключался
			s.clientsMapMutex.Lock()
			if lobby == nil {
				lobby = new(Lobby)
				*lobby = Lobby{
					Info:            res.Data,
					expectingPlayer: c,
					isPlaying:       false,
					channel:         make(chan string, 1),
					results:         make(chan result, 1),
				}
			} else {
				lobby.expectingPlayer = c
			}
			s.playingLobbies[uint(i)] = lobby
			s.connectedClient[c] = lobby
			s.clientsMapMutex.Unlock()
			c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
		} else if lobby.isPlaying {
			//Лобби создано, но там уже кто-то играет
			fmt.Printf("Player %s trying join lobby that already playing game\n", c.name)
			c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
			s.clientsMapMutex.Lock()
			s.connectedClient[c] = nil
			s.clientsMapMutex.Unlock()
		} else if lobby.expectingPlayer != nil {
			//Лобби создано и там кто-то ждёт
			c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
			s.connectedClient[c] = lobby
			time.Sleep(1 * time.Second)
			go func() {
				lobby.playGame(c, lobby.expectingPlayer)
				gameResult := <-lobby.results
				s.deleteLobby(gameResult, lobby, c, lobby.expectingPlayer)
			}()
		}
		s.lobbiesMutex.Unlock()
	}
}

func (s *server) disconnect(c *connectedClient) {
	s.clientsMapMutex.Lock()
	if lobby, ok := s.connectedClient[c]; ok && lobby != nil {
		lobby.removePlayer(c)
	}
	s.clientsMapMutex.Unlock()
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
}

func (s *server) getLobbies(c *connectedClient) {
	//s.updateLobbies()
	var infos = make([]LobbyInfo, 0, MaxPlayers*(MaxPlayers-1)/2*s.gamesToPlay)
	var getLobbyResponse GetLobbyResponse
	rows, err := s.db.Query("SELECT * from lobbies")
	if err != nil {
		getLobbyResponse = GetLobbyResponse{
			Data:    nil,
			Success: false,
		}
	} else {
		defer rows.Close()
		for rows.Next() {
			var lobbyInfo LobbyInfo
			err2 := rows.Scan(&lobbyInfo.ID, &lobbyInfo.Width, &lobbyInfo.Height, &lobbyInfo.GameBarrierCount, &lobbyInfo.PlayerBarrierCount, &lobbyInfo.Name, &lobbyInfo.PlayersCount)
			if err2 != nil {
				logError(595, err2.Error())
			}
			infos = append(infos, lobbyInfo)
		}
		getLobbyResponse = GetLobbyResponse{
			Data:    infos[:],
			Success: true,
		}
	}
	data, _ := json.Marshal(getLobbyResponse)
	c.SendData([]byte(fmt.Sprintf("%s\n", string(data))))
}

//Читает настройки
func readConfigs() configs {
	file, err := os.Open("resources/config.json")
	if err != nil {
		panic(err)
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

func logError(line int, string2 string) {
	fmt.Printf("Error at line %d: %s\n", line, string2)
}

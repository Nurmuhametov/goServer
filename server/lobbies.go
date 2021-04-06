package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"strings"
	"time"
)

type result struct {
	first  string
	second string
	result string
}

//Структура представляющая лобби
type Lobby struct {
	Info            LobbyInfo        //Параметры данного лобби, такие как ширина, высота, количество препятствий
	expectingPlayer *connectedClient //Ожидающий в лобби клиент
	isPlaying       bool             //Идёт ли игра в данном лобби в данный момент
	server          *Server          //Ссылка на сервер
	channel         chan string      //Канал, в который игроки пишут свои ходы
	results         chan result      //Канал, в который отправятся результаты после окончания игры
}

//Таймаут ходов
const Timeout = 120

//Наибольшее количество ходов, после которого будет объявлена ничья
const MaxTurns = 30

//Удаляет клиента из лобби
func (l *Lobby) removePlayer(client *connectedClient) {
	if l.isPlaying {
		return
	}
	if l.expectingPlayer == client {
		l.expectingPlayer = nil
	}
}

//Добавляет клиента в лобби, если в лобби уже есть ожидающий клиент, то начинает игру между ними
func (l *Lobby) AddPLayer(client *connectedClient, ch chan error) {
	if l.isPlaying {
		ch <- errors.New("trying to join lobby that already started a game")
		return
	} else {
		ch <- nil
	}
	_ = <-ch
	if l.expectingPlayer == nil {
		l.expectingPlayer = client
	} else {
		l.isPlaying = true
		//l.expectingPlayer.Lobby.opponent = client
		//client.Lobby.opponent = l.expectingPlayer
		go l.playGame(l.expectingPlayer, client)
		var res = <-l.results
		l.server.deleteLobby(res, l, l.expectingPlayer, client)
	}
}

//Основной метод, который проводит игру между клиентами
func (l *Lobby) playGame(player1 *connectedClient, player2 *connectedClient) {
	fmt.Printf("Game between %s and %s started!\n", player1.name, player2.name)
	var first, second = func() (*connectedClient, *connectedClient) {
		if rand.Uint32()&1 == 0 {
			return player1, player2
		} else {
			return player2, player1
		}
	}()
	first.AddListener(getTurn)
	second.AddListener(getTurn)
	//fmt.Printf("First: my opponent is %s\n", first.Lobby.opponent.name)
	//fmt.Printf("Second: my opponent is %s\n", second.Lobby.opponent.name)
	var field = l.generateRandomField()
	var startGameInfo = StartGameInfo{
		Move:             true,
		Width:            l.Info.Width,
		Height:           l.Info.Height,
		Position:         field.Position,
		OpponentPosition: field.OpponentPosition,
		Barriers:         field.Barriers,
	}
	data, _ := json.Marshal(startGameInfo)
	first.SendData([]byte(fmt.Sprintf("SOCKET STARTGAME %s\n", string(data))))
	startGameInfo.Move = false
	startGameInfo.Position = field.OpponentPosition
	startGameInfo.OpponentPosition = field.Position
	data, _ = json.Marshal(startGameInfo)
	second.SendData([]byte(fmt.Sprintf("SOCKET STARTGAME %s\n", string(data))))
	var ch = make(chan *connectedClient, 1)
	file, err := os.Open("resources/template.html")
	var log = make([]byte, 1024*100)
	if err != nil {
		println(err.Error())
	} else {
		_, err2 := file.Read(log)
		if err2 != nil {
			println(err2.Error())
		}
	}
	re := regexp.MustCompile("<!--NAME-->")
	log = re.ReplaceAll(log, []byte(fmt.Sprintf("%s vs %s %s", first.name, second.name, time.Now().Format(time.RFC822))))
	re = regexp.MustCompile("<!--GAME NAME-->")
	log = re.ReplaceAll(log, []byte(fmt.Sprintf("Первый игрок - %s, второй игрок %s", first.name, second.name)))
	go func() {
		x := 0
		leader, follower := first, second
		for {
			select {
			//Если ответ пришёл вовремя
			case res := <-l.channel:
				err := json.Unmarshal([]byte(res), &field)
				//Если получен ответ в неверном формате
				if err != nil {
					re = regexp.MustCompile("<!--COMMENTS-->")
					log = re.ReplaceAll(log, []byte(fmt.Sprintf("Победил игрок %s так как игрок %s не смог прислать данные в верном формате\n", follower.name, leader.name)))
					ch <- follower
					return
				} else { // Ответ в нормальном формате
					if winner, ok := isEnded(x, field, first, second, first.name == follower.name); !ok { //Если игра не закончилась
						swap(&field)
						d, _ := json.Marshal(field)
						follower.SendData([]byte(fmt.Sprintf("SOCKET STEP %s\n", string(d))))
						l.writeToLog(&log, &field, x, first.name == follower.name)
						leader, follower = follower, leader
						x += 1
					} else { //Если закончилась
						ch <- winner
						return
					}
				}
			//Если ответ не пришёл вовремя
			case <-time.After(Timeout * time.Second):
				re = regexp.MustCompile("<!--COMMENTS-->")
				log = re.ReplaceAll(log, []byte(fmt.Sprintf("Победил игрок %s так как игрок %s не ответил вовремя\n", follower.name, leader.name)))
				ch <- follower
				return
			}
		}
	}()
	var winner = <-ch
	_, _ = funcPop(&first.dataReceivedListeners)
	_, _ = funcPop(&second.dataReceivedListeners)
	var endGame EndGameInfo
	first.readMutex.Lock()
	second.readMutex.Lock()
	if winner == nil { //Ничья
		re = regexp.MustCompile("<!--RESULT-->")
		log = re.ReplaceAll(log, []byte(fmt.Sprintf("Ничья!")))
		l.results <- result{
			first:  first.name,
			second: second.name,
			result: "draw",
		}
		endGame = EndGameInfo{
			Result:           "draw",
			Width:            l.Info.Width,
			Height:           l.Info.Height,
			Position:         field.Position,
			OpponentPosition: field.OpponentPosition,
			Barriers:         field.Barriers,
		}
		res, _ := json.Marshal(endGame)
		first.SendData([]byte(fmt.Sprintf("SOCKET ENDGAME %s\n", string(res))))
		second.SendData([]byte(fmt.Sprintf("SOCKET ENDGAME %s\n", string(res))))
	} else if winner.name == first.name {
		re = regexp.MustCompile("<!--RESULT-->")
		log = re.ReplaceAll(log, []byte(fmt.Sprintf("Победил игрок %s", winner.name)))
		l.results <- result{
			first:  first.name,
			second: second.name,
			result: "win",
		}
		endGame = EndGameInfo{
			Result:           "win",
			Width:            l.Info.Width,
			Height:           l.Info.Height,
			Position:         field.Position,
			OpponentPosition: field.OpponentPosition,
			Barriers:         field.Barriers,
		}
		res, _ := json.Marshal(endGame)
		first.SendData([]byte(fmt.Sprintf("SOCKET ENDGAME %s\n", string(res))))
		endGame.Result = "lose"
		endGame.Position, endGame.OpponentPosition = endGame.OpponentPosition, endGame.Position
		res, _ = json.Marshal(endGame)
		second.SendData([]byte(fmt.Sprintf("SOCKET ENDGAME %s\n", string(res))))
	} else {
		re = regexp.MustCompile("<!--RESULT-->")
		log = re.ReplaceAll(log, []byte(fmt.Sprintf("Победил игрок %s", winner.name)))
		l.results <- result{
			first:  first.name,
			second: second.name,
			result: "lose",
		}
		endGame = EndGameInfo{
			Result:           "win",
			Width:            l.Info.Width,
			Height:           l.Info.Height,
			Position:         field.Position,
			OpponentPosition: field.OpponentPosition,
			Barriers:         field.Barriers,
		}
		res, _ := json.Marshal(endGame)
		second.SendData([]byte(fmt.Sprintf("SOCKET ENDGAME %s\n", string(res))))
		endGame.Result = "lose"
		endGame.Position, endGame.OpponentPosition = endGame.OpponentPosition, endGame.Position
		res, _ = json.Marshal(endGame)
		first.SendData([]byte(fmt.Sprintf("SOCKET ENDGAME %s\n", string(res))))
	}
	log = bytes.Trim(log, "\x00")
	err = os.WriteFile(fmt.Sprintf("logs/%s_vs_ %s_%s.html", first.name, second.name, time.Now().Format(time.StampMicro)), log, 0644)
	if err != nil {
		println(err.Error())
	}
}

//Меняет местами позиции игроков в поле
func swap(f *Field) {
	f.Position, f.OpponentPosition = f.OpponentPosition, f.Position
}

//Проверяет, зкаончилась ли игра на данном этапе. x - номер текущего хода, f - состояние поля, first, second -
//клиенты в том порядке, в котором начали игру, swapped - если поле присланно вторым клиентом
func isEnded(x int, f Field, first, second *connectedClient, swapped bool) (*connectedClient, bool) {
	var player1, player2 [2]uint8
	if swapped {
		player1 = f.OpponentPosition
		player2 = f.Position
	} else {
		player1 = f.Position
		player2 = f.OpponentPosition
	}
	if player1[0] == f.Height-1 {
		return first, true
	}
	if player2[0] == 0 {
		return second, true
	}
	if x >= MaxTurns {
		return nil, true
	}
	return nil, false
}

//Генерирует случайное допустимое поле
func (l *Lobby) generateRandomField() Field {
	var position = [2]uint8{0, uint8(rand.Uint32()) % l.Info.Width}
	var opponentPosition = [2]uint8{l.Info.Height - 1, uint8(rand.Uint32()) % l.Info.Width}
	barriers := generateBarriers(position, opponentPosition, l.Info.GameBarrierCount, l.Info.Width, l.Info.Height)
	var field = Field{
		Width:            l.Info.Width,
		Height:           l.Info.Height,
		Position:         position,
		OpponentPosition: opponentPosition,
		Barriers:         barriers,
	}
	return field
}

func (l *Lobby) writeToLog(log *[]byte, f *Field, x int, swapped bool) {
	re := regexp.MustCompile("<!--TURNS-->")
	var table = make([]byte, f.Width*f.Height)
	var player1 = func() [2]uint8 {
		if swapped {
			return f.OpponentPosition
		} else {
			return f.Position
		}
	}()
	var player2 = func() [2]uint8 {
		if !swapped {
			return f.OpponentPosition
		} else {
			return f.Position
		}
	}()
	table[player1[0]*f.Width+player1[1]] = 1
	table[player2[0]*f.Width+player2[1]] = 2
	for _, val := range f.Barriers {
		// 1 << 2 - top
		// 1 << 3 - bot
		// 1 << 4 - left
		// 1 << 5 - right
		var rest1, rest2 = defineRestrictions(val[0], val[1])
		table[val[0][0]*f.Width+val[0][1]] += rest1
		table[val[1][0]*f.Width+val[1][1]] += rest2
		rest1, rest2 = defineRestrictions(val[2], val[3])
		table[val[2][0]*f.Width+val[2][1]] += rest1
		table[val[3][0]*f.Width+val[3][1]] += rest2
	}
	var str strings.Builder
	str.WriteString(fmt.Sprintf("<p>Ход номер %d</p>\n<table>\n", x+1))
	for i := uint8(0); i < f.Height; i++ {
		str.WriteString("<tr>")
		for j := uint8(0); j < f.Width; j++ {
			str.WriteString(getStrFromAttr(table[i*f.Width+j]))
		}
		str.WriteString("</tr>\n")
	}
	str.WriteString("</table>\n<!--TURNS-->")
	*log = re.ReplaceAll(*log, []byte(str.String()))

}

func getStrFromAttr(b byte) string {
	// 1 << 2 - top
	// 1 << 3 - bot
	// 1 << 4 - left
	// 1 << 5 - right
	switch b {
	case 0:
		return "<td></td>"
	case 1:
		return "<td class=\"player1\">1</td>"
	case 2:
		return "<td class=\"player2\">2</td>"
	case 1 << 2:
		return "<td class=\"top\"></td>"
	case 1 << 3:
		return "<td class=\"bottom\"></td>"
	case 1 << 4:
		return "<td class=\"left\"></td>"
	case 1 << 5:
		return "<td class=\"right\"></td>"
	case 1<<2 + 1:
		return "<td class=\"top player1\">1</td>"
	case 1<<3 + 1:
		return "<td class=\"bottom player1\">1</td>"
	case 1<<4 + 1:
		return "<td class=\"left player1\">1</td>"
	case 1<<5 + 1:
		return "<td class=\"right player1\">1</td>"
	case 1<<2 + 2:
		return "<td class=\"top player2\">2</td>"
	case 1<<3 + 2:
		return "<td class=\"bottom player2\">2</td>"
	case 1<<4 + 2:
		return "<td class=\"left player2\">2</td>"
	case 1<<5 + 2:
		return "<td class=\"right player2\">2</td>"
	default:
		return "<td></td>"
	}
}

func defineRestrictions(first [2]uint8, second [2]uint8) (uint8, uint8) {
	switch first[0] - second[0] {
	case 1:
		return 1 << 2, 1 << 3
	case 255:
		return 1 << 3, 1 << 2
	case 0:
		switch first[1] - second[1] {
		case 1:
			return 1 << 4, 1 << 5
		case 255:
			return 1 << 5, 1 << 4
		}
	}
	return 0, 0
}

//Генерирует препятствия для поля
func generateBarriers(position, opponentPosition [2]uint8, count, width, height uint8) [][4][2]uint8 {
	var res = make([][4][2]uint8, 0, count)
	for true {
		var y = uint8(rand.Uint32()) % height
		var x = uint8(rand.Uint32()) % width
		var dir = uint8(rand.Uint32()) % 8
		newBarrier := randomBarrier(x, y, dir)
		if !isValidObstacle(newBarrier, width, height) {
			continue
		}
		if isStepOver(newBarrier[0], newBarrier[1], res) || isStepOver(newBarrier[2], newBarrier[3], res) {
			continue
		}
		newSetBarriers := append(res, newBarrier)
		if !isPathExists(position, newSetBarriers, width, height) || !isPathExists(opponentPosition, newSetBarriers, width, height) {
			continue
		}
		res = append(res, newBarrier)
		if uint8(len(res)) == count {
			break
		}
	}
	return res
}

//Проверяет, существует ли путь из position до противпоположного конца поля
func isPathExists(position [2]uint8, barriers [][4][2]uint8, width, height uint8) bool {
	var goal = func() uint8 {
		if position[0] == 0 {
			return height - 1
		} else {
			return 0
		}
	}()
	if position[0] == goal {
		return true
	}
	var positions = new(PositionStack)
	positions = nil
	posPush(&positions, position)
	var visitedCells = make([]bool, width*height, width*height)
	visitedCells[position[0]*width+position[1]] = true
	for {
		var current, ok = posPop(&positions)
		if !ok {
			break
		}
		var moves = expandMoves(current, barriers, width, height, goal)
		for _, val := range moves {
			if val[0] == goal {
				return true
			}
			if !(val[0] == current[0] && val[1] == current[1]) && !visitedCells[val[0]*width+val[1]] {
				posPush(&positions, val)
				visitedCells[val[0]*width+val[1]] = true
			}
		}
	}
	return false
}

//Получает список доступных ходов
func expandMoves(pos [2]uint8, barriers [][4][2]uint8, width, height, goal uint8) [][2]uint8 {
	var res = make([][2]uint8, 0, 4)
	var moves = func() [4][2]uint8 {
		if goal != 0 {
			return [4][2]uint8{
				{pos[0] + 1, pos[1]},
				{pos[0], pos[1] + 1},
				{pos[0], pos[1] - 1},
				{pos[0] - 1, pos[1]},
			}
		} else {
			return [4][2]uint8{
				{pos[0] - 1, pos[1]},
				{pos[0], pos[1] + 1},
				{pos[0], pos[1] - 1},
				{pos[0] + 1, pos[1]},
			}
		}
	}()
	for _, val := range moves {
		if val[0] < height && val[1] < width && !isStepOver(pos, val, barriers) {
			res = append(res, val)
		}
	}
	return res
}

//Проверяет, пересекает ли ход из from в to одно из препятствий
func isStepOver(from, to [2]uint8, barriers [][4][2]uint8) bool {
	for i := 0; i < len(barriers); i++ {
		if from[0] == barriers[i][0][0] && from[1] == barriers[i][0][1] && to[0] == barriers[i][1][0] && to[1] == barriers[i][1][1] ||
			from[0] == barriers[i][2][0] && from[1] == barriers[i][2][1] && to[0] == barriers[i][3][0] && to[1] == barriers[i][3][1] ||
			to[0] == barriers[i][0][0] && to[1] == barriers[i][0][1] && from[0] == barriers[i][1][0] && from[1] == barriers[i][1][1] ||
			to[0] == barriers[i][2][0] && to[1] == barriers[i][2][1] && from[0] == barriers[i][3][0] && from[1] == barriers[i][3][1] {
			return true
		}
	}
	return false
}

//Проверяет, ставится ли препятствие в пределах поля
func isValidObstacle(barrier [4][2]uint8, width, height uint8) bool {
	if barrier[0][0] < 0 || barrier[0][0] >= height || barrier[0][1] < 0 || barrier[0][1] >= width ||
		barrier[1][0] < 0 || barrier[1][0] >= height || barrier[1][1] < 0 || barrier[1][1] >= width ||
		barrier[2][0] < 0 || barrier[2][0] >= height || barrier[2][1] < 0 || barrier[2][1] >= width ||
		barrier[3][0] < 0 || barrier[3][0] >= height || barrier[3][1] < 0 || barrier[3][1] >= width {
		return false
	}
	return true
}

//Генерирует случаное препятствие в точке (x,y), dir in [0,7] - одно из восьми возможных направлений
func randomBarrier(x, y, dir uint8) [4][2]uint8 {
	switch dir {
	case 0:
		return [4][2]uint8{{x, y}, {x + 1, y}, {x, y - 1}, {x + 1, y - 1}}
	case 1:
		return [4][2]uint8{{x, y}, {x + 1, y}, {x, y + 1}, {x + 1, y + 1}}
	case 2:
		return [4][2]uint8{{x, y}, {x - 1, y}, {x, y - 1}, {x - 1, y - 1}}
	case 3:
		return [4][2]uint8{{x, y}, {x - 1, y}, {x, y + 1}, {x - 1, y + 1}}
	case 4:
		return [4][2]uint8{{x, y}, {x, y + 1}, {x + 1, y}, {x + 1, y + 1}}
	case 5:
		return [4][2]uint8{{x, y}, {x, y - 1}, {x + 1, y}, {x + 1, y - 1}}
	case 6:
		return [4][2]uint8{{x, y}, {x, y + 1}, {x - 1, y}, {x - 1, y + 1}}
	case 7:
		return [4][2]uint8{{x, y}, {x, y - 1}, {x - 1, y}, {x - 1, y - 1}}
	default:
		return [4][2]uint8{{x, y}, {x + 1, y}, {x, y - 1}, {x + 1, y - 1}}
	}
}

//Вызывается при получении сообщения от клиента, который находится в состоянии игры. Записывает сообщение в канал лобби
func getTurn(str string, client *connectedClient) {
	data := strings.TrimPrefix(str, "SOCKET STEP")
	//fmt.Printf("Got from %s: %s\n", client.name, str)
	client.Lobby.channel <- data
}

//Генерирует лобби с соответствующими параметрами
func GetLobby(info LobbyInfo) *Lobby {
	var res = new(Lobby)
	*res = Lobby{
		Info:            info,
		expectingPlayer: nil,
		isPlaying:       false,
		channel:         make(chan string),
		results:         make(chan result, 1),
	}
	return res
}

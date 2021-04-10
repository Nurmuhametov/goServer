package thinker

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"socrates/utils"
	"strings"
)

type Thinker struct {
	name           string
	writer         *bufio.Writer
	reader         *bufio.Reader
	commandsBuffer chan string
	isActive       bool
}

func InitThinker(name string, conn net.Conn) *Thinker {
	var res = new(Thinker)
	res.name = name
	res.reader = bufio.NewReader(conn)
	res.writer = bufio.NewWriter(conn)
	res.commandsBuffer = make(chan string, 1)
	res.isActive = true
	go res.receiveMessages()
	err := res.login()
	if err != nil {
		panic(err)
	}
	return res
}

func (t *Thinker) PlayGame() {
	err := t.sendCommand("SOCKET JOINLOBBY {\"id\":null}")
	if err != nil {
		println(err.Error())
	}
	res := <-t.commandsBuffer
	var joinLobbyResponse utils.JoinLobbyResponse
	err = json.Unmarshal([]byte(res), &joinLobbyResponse)
	if err != nil {
		println(err.Error())
	}
	res = <-t.commandsBuffer
	//fmt.Printf("Command: %s\n", res)
	var startGameInfo utils.StartGameInfo
	res = strings.TrimPrefix(res, "SOCKET STARTGAME")
	err = json.Unmarshal([]byte(res), &startGameInfo)
	game := utils.Game{
		Goal: func() uint8 {
			if startGameInfo.Position[0] == 0 {
				return startGameInfo.Height - 1
			} else {
				return 0
			}
		}(),
		GameBarrierCount:   joinLobbyResponse.Data.GameBarrierCount,
		PlayerBarrierCount: joinLobbyResponse.Data.PlayerBarrierCount,
		Width:              startGameInfo.Width,
		Height:             startGameInfo.Height,
		Position:           startGameInfo.Position,
		OpponentPosition:   startGameInfo.OpponentPosition,
		Barriers:           startGameInfo.Barriers,
	}
	if startGameInfo.Move {
		//println("I'm first!")
		t.makeMove(&game)
	}
	for {
		isEnded, result := t.waitTurn(&game)
		if !isEnded {
			t.makeMove(&game)
		} else {
			println(result)
			break
		}
	}
}

func (t *Thinker) login() error {
	err := t.sendCommand("CONNECTION {\"LOGIN\":\"" + t.name + "\"}")
	if err != nil {
		println("Cannot send CONNECTION message")
		return err
	}
	response := <-t.commandsBuffer
	var msg utils.Message
	err = json.Unmarshal([]byte(response), &msg)
	if err != nil {
		println("Cannot unmarshall CONNECTION message")
		return err
	}
	if msg.Msg == "LOGIN OK" {
		return nil
	} else {
		return errors.New("wrong login")
	}
}

func (t *Thinker) receiveMessages() {
	for t.isActive {
		buff, isPrefix, err := t.reader.ReadLine()
		if err != nil {
			println(err.Error())
			return
		}
		//fmt.Printf("Received: %s\n", string(buff))
		t.commandsBuffer <- string(buff)
		for isPrefix {
			//fmt.Printf("Received as suffix: %s\n", string(buff))
			buff, isPrefix, err = t.reader.ReadLine()
			if err != nil {
				println(err.Error())
				return
			}
			t.commandsBuffer <- string(buff)
		}
	}
}

func (t *Thinker) sendCommand(str string) error {
	_, err := t.writer.WriteString(str)
	_ = t.writer.Flush()
	if err != nil {
		println("Cannot send %s message", str)
		return err
	}
	//fmt.Printf("Sent: %s\n", str)
	return nil
}

func (t *Thinker) makeMove(game *utils.Game) {
	//fmt.Printf("Я сейчас в [%d, %d], противник в [%d, %d]\n", game.Position[0], game.Position[1], game.OpponentPosition[0], game.OpponentPosition[1])
	moves := expandMoves(game)
	var obstacles [][4][2]uint8
	var move string
	if game.PlayerBarrierCount > 0 {
		obstacles = expandObstacles(game)
	}
	if len(moves)+len(obstacles) == 0 {
		//fmt.Println("Мне некуда пойти!")
	} else {
		moveNumber := chooseMove(moves, obstacles)
		if moveNumber >= len(moves) {
			//fmt.Println("Ставлю препятствие")
			if obstacles == nil {
				panic("obstacles is empty!")
			}
			var obstacle = obstacles[moveNumber-len(moves)]
			game.Barriers = append(game.Barriers, obstacle)
			game.PlayerBarrierCount -= 1
		} else {
			//fmt.Println("Перемещаюсь")
			var position = moves[moveNumber]
			game.Position = position
		}
	}
	var field = utils.Field{
		Width:            game.Width,
		Height:           game.Height,
		Position:         game.Position,
		OpponentPosition: game.OpponentPosition,
		Barriers:         game.Barriers,
	}
	data, _ := json.Marshal(field)
	move = fmt.Sprintf("SOCKET STEP %s\n", string(data))
	game.Turn += 1
	err := t.sendCommand(move)
	if err != nil {
		println(err.Error())
	}
}

func chooseMove(moves [][2]uint8, obstacles [][4][2]uint8) int {
	return 0
}

func (t *Thinker) waitTurn(game *utils.Game) (bool, string) {
	//fmt.Printf("Жду свой ход\n")
	step := <-t.commandsBuffer
	//if err != nil {
	//	println(err.Error())
	//}
	re := regexp.MustCompile("[A-Z ]+[A-Z]|(?:{.+})")
	split := re.FindAllString(step, 2)
	if len(split) == 2 && split[0] == "SOCKET STEP" {
		var field utils.Field
		_ = json.Unmarshal([]byte(split[1]), &field)
		game.Width = field.Width
		game.Height = field.Height
		game.Position = field.Position
		game.OpponentPosition = field.OpponentPosition
		game.Barriers = field.Barriers
		game.Turn += 1
		return false, ""
	} else if len(split) == 2 && split[0] == "SOCKET ENDGAME" {
		var endGameInfo utils.GameResultInfo
		_ = json.Unmarshal([]byte(split[1]), &endGameInfo)
		game.OpponentPosition = endGameInfo.OpponentPosition
		game.Barriers = endGameInfo.Barriers
		game.Turn += 1
		return true, endGameInfo.Result
	}
	panic("watafak")
}

func expandObstacles(game *utils.Game) [][4][2]uint8 {
	res := make([][4][2]uint8, 0, game.Width*game.Height*8)
	for i := uint8(0); i < game.Height; i++ {
		for j := uint8(0); j < game.Width; j++ {
			for k := uint8(0); k < 8; k++ {
				obstacle := getBarrier(i, j, k)
				if !isValidObstacle(obstacle, game.Width, game.Height) {
					continue
				}
				if isStepOver(obstacle[0], obstacle[1], game.Barriers) || isStepOver(obstacle[2], obstacle[3], game.Barriers) {
					continue
				}
				newSetBarriers := append(res, obstacle)
				if !isPathExists(game.Position, newSetBarriers, game.Goal, game.Width, game.Height) || !isPathExists(game.OpponentPosition, newSetBarriers, game.Height-1-game.Goal, game.Width, game.Height) {
					continue
				}
				res = append(res, obstacle)
			}
		}
	}
	return res[:]
}

func isPathExists(position [2]uint8, obstacles [][4][2]uint8, goal, width, height uint8) bool {
	if position[0] == goal {
		return true
	}
	var positions = new(utils.PositionStack)
	positions = nil
	utils.PosPush(&positions, position)
	var visitedCells = make([]bool, width*height, width*height)
	visitedCells[position[0]*width+position[1]] = true
	for {
		var current, ok = utils.PosPop(&positions)
		if !ok {
			break
		}
		var moves = getMoves(current, obstacles, width, height, goal)
		for _, val := range moves {
			if val[0] == goal {
				return true
			}
			if !(val[0] == current[0] && val[1] == current[1]) && !visitedCells[val[0]*width+val[1]] {
				utils.PosPush(&positions, val)
				visitedCells[val[0]*width+val[1]] = true
			}
		}
	}
	return false
}

func getMoves(current [2]uint8, obstacles [][4][2]uint8, width, height, goal uint8) [][2]uint8 {
	res := make([][2]uint8, 0, 4)
	var moves = func() [4][2]uint8 {
		if goal != 0 {
			return [4][2]uint8{
				{current[0] + 1, current[1]},
				{current[0], current[1] + 1},
				{current[0], current[1] - 1},
				{current[0] - 1, current[1]},
			}
		} else {
			return [4][2]uint8{
				{current[0] - 1, current[1]},
				{current[0], current[1] + 1},
				{current[0], current[1] - 1},
				{current[0] + 1, current[1]},
			}
		}
	}()
	for i := 0; i < 4; i++ {
		if moves[i][0] < height && moves[i][1] < width && !isStepOver(current, moves[i], obstacles) {
			res = append(res, moves[i])
		}
	}
	return res[:]
}

func isValidObstacle(obstacle [4][2]uint8, width, height uint8) bool {
	if obstacle[0][0] < 0 || obstacle[0][0] >= height || obstacle[0][1] < 0 || obstacle[0][1] >= width ||
		obstacle[1][0] < 0 || obstacle[1][0] >= height || obstacle[1][1] < 0 || obstacle[1][1] >= width ||
		obstacle[2][0] < 0 || obstacle[2][0] >= height || obstacle[2][1] < 0 || obstacle[2][1] >= width ||
		obstacle[3][0] < 0 || obstacle[3][0] >= height || obstacle[3][1] < 0 || obstacle[3][1] >= width {
		return false
	}
	return true
}

func getBarrier(x, y, dir uint8) [4][2]uint8 {
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

func expandMoves(game *utils.Game) [][2]uint8 {
	var moves [4][2]uint8
	res := make([][2]uint8, 0, 4)
	moves[0] = [2]uint8{game.Position[0] + 1, game.Position[1]}
	moves[1] = [2]uint8{game.Position[0], game.Position[1] + 1}
	moves[2] = [2]uint8{game.Position[0], game.Position[1] - 1}
	moves[3] = [2]uint8{game.Position[0] - 1, game.Position[1]}
	for i := 0; i < 4; i++ {
		if isLegalMove(game.Position, moves[i], game) {
			res = append(res, moves[i])
		}
	}
	return res
}

func isLegalMove(from, to [2]uint8, game *utils.Game) bool {
	if to[0] < 0 || to[0] >= game.Height || to[1] < 0 || to[1] >= game.Width {
		//fmt.Printf("Я не могу сходить из [%d, %d] в [%d, %d] потому что за пределами поля\n", game.Position[0], game.Position[1], to[0], to[1])
		return false
	}
	if to[0] == game.OpponentPosition[0] && to[1] == game.OpponentPosition[1] {
		//fmt.Printf("Я не могу сходить из [%d, %d] в [%d, %d] потому что сяду на лицо противнику\n", game.Position[0], game.Position[1], to[0], to[1])
		return false
	}
	if isStepOver(from, to, game.Barriers) {
		//fmt.Printf("Я не могу сходить из [%d, %d] в [%d, %d] потому что там препятствие\n", game.Position[0], game.Position[1], to[0], to[1])
		return false
	}
	return true
}

func isStepOver(from, to [2]uint8, b [][4][2]uint8) bool {
	for _, val := range b {
		if (from[0] == val[0][0] && from[1] == val[0][1] && to[0] == val[1][0] && to[1] == val[1][1]) ||
			(from[0] == val[2][0] && from[1] == val[2][1] && to[0] == val[3][0] && to[1] == val[3][1]) ||
			(from[0] == val[1][0] && from[1] == val[1][1] && to[0] == val[0][0] && to[1] == val[0][1]) ||
			(from[0] == val[3][0] && from[1] == val[3][1] && to[0] == val[2][0] && to[1] == val[2][1]) {
			return true
		}
	}
	return false
}

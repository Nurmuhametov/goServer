package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"
)

type Lobby struct {
	Info            LobbyInfo
	expectingPlayer *ConnectedClient
	isPlaying       bool
	opponent        *ConnectedClient
	channel         chan string
}

const Timeout = 120
const MaxTurns = 30

func (l *Lobby) removePlayer(client *ConnectedClient) {
	if l.isPlaying {
		return
	}
	if l.expectingPlayer == client {
		l.expectingPlayer = nil
	}
}

func (l *Lobby) AddPLayer(client *ConnectedClient, ch chan error) {
	if l.isPlaying {
		ch <- errors.New("trying to join lobby that already started a game")
		return
	} else {
		ch <- nil
	}
	_ = <-ch
	fmt.Printf("Player %s joined lobby %s at address %d\n", client.name, l.Info.Name, &l)
	if l.expectingPlayer == nil {
		l.expectingPlayer = client
	} else {
		l.isPlaying = true
		l.expectingPlayer.Lobby.opponent = client
		client.Lobby.opponent = l.expectingPlayer
		go l.playGame(l.expectingPlayer, client)
	}
}

func (l *Lobby) playGame(player1 *ConnectedClient, player2 *ConnectedClient) {
	fmt.Printf("Game between %s and %s started!\n", player1.name, player2.name)
	var first, second = func() (*ConnectedClient, *ConnectedClient) {
		if rand.Uint32()&1 == 0 {
			return player1, player2
		} else {
			return player2, player1
		}
	}()
	first.AddListener(getTurn)
	second.AddListener(getTurn)
	var field = l.generateRandomField()
	var startGameInfo = StartGameInfo{
		Move:             true,
		Width:            l.Info.Width,
		Height:           l.Info.Height,
		Position:         field.Position,
		OpponentPosition: field.OpponentPosition,
		Barriers:         field.Barriers,
	}
	var startStr = []byte("SOCKET STARTGAME ")
	data, _ := json.Marshal(startGameInfo)
	first.SendData(append(startStr, data...))
	startGameInfo.Move = false
	startGameInfo.Position = field.OpponentPosition
	startGameInfo.OpponentPosition = field.Position
	data, _ = json.Marshal(startGameInfo)
	second.SendData(append(startStr, data...))
	var ch = make(chan *ConnectedClient, 1)
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
					ch <- follower
					return
				} else { // Ответ в нормальном формате
					if winner, ok := isEnded(x, field, first, second, first.name == follower.name); !ok { //Если игра не закончилась
						swap(&field)
						d, _ := json.Marshal(field)
						follower.SendData([]byte(fmt.Sprintf("SOCKET STEP %s\n", string(d))))
						//TODO(Log)
						fmt.Printf("Turn %d, situation %s\n", x+1, res)
						leader, follower = follower, leader
						x += 1
					} else { //Если закончилась
						ch <- winner
						return
					}
				}
			//Если ответ не пришёл вовремя
			case <-time.After(Timeout * time.Second):
				ch <- follower
				return
			}
		}
	}()
	var winner = <-ch
	_, _ = funcPop(&first.dataReceivedListeners)
	_, _ = funcPop(&second.dataReceivedListeners)
	first.Lobby = nil
	second.Lobby = nil
	var endGame EndGameInfo
	if winner == nil { //Ничья
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
}

func swap(f *Field) {
	f.Position, f.OpponentPosition = f.OpponentPosition, f.Position
}

func isEnded(x int, f Field, first, second *ConnectedClient, swapped bool) (*ConnectedClient, bool) {
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

func isValidObstacle(barrier [4][2]uint8, width, height uint8) bool {
	if barrier[0][0] < 0 || barrier[0][0] >= height || barrier[0][1] < 0 || barrier[0][1] >= width ||
		barrier[1][0] < 0 || barrier[1][0] >= height || barrier[1][1] < 0 || barrier[1][1] >= width ||
		barrier[2][0] < 0 || barrier[2][0] >= height || barrier[2][1] < 0 || barrier[2][1] >= width ||
		barrier[3][0] < 0 || barrier[3][0] >= height || barrier[3][1] < 0 || barrier[3][1] >= width {
		return false
	}
	return true
}

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

func getTurn(str string, client *ConnectedClient) {
	//TODO()
	fmt.Printf("Got from %s: %s", client.name, str)
	data := strings.TrimPrefix(str, "SOCKET STEP")
	client.Lobby.channel <- data
}

func GetLobby(info LobbyInfo) *Lobby {
	var res = new(Lobby)
	*res = Lobby{
		Info:            info,
		expectingPlayer: nil,
		isPlaying:       false,
		opponent:        nil,
		channel:         make(chan string, 1),
	}
	return res
}

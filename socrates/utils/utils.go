package utils

type Message struct {
	Msg string `json:"MESSAGE"`
}

type JoinLobbyResponse struct {
	Data    LobbyInfo `json:"DATA"`
	Success bool      `json:"SUCCESS"`
}

type LobbyInfo struct {
	ID                 string `json:"_id"`
	Width              uint8  `json:"width"`
	Height             uint8  `json:"height"`
	GameBarrierCount   uint8  `json:"gameBarrierCount"`
	PlayerBarrierCount uint8  `json:"playerBarrierCount"`
	Name               string `json:"name"`
	PlayersCount       uint8  `json:"players_count"`
}

type StartGameInfo struct {
	Move             bool          `json:"move"`
	Width            uint8         `json:"width"`
	Height           uint8         `json:"height"`
	Position         [2]uint8      `json:"position"`
	OpponentPosition [2]uint8      `json:"opponentPosition"`
	Barriers         [][4][2]uint8 `json:"barriers"`
}

type Game struct {
	Turn               uint8
	Goal               uint8
	GameBarrierCount   uint8
	PlayerBarrierCount uint8
	Width              uint8
	Height             uint8
	Position           [2]uint8
	OpponentPosition   [2]uint8
	Barriers           [][4][2]uint8
}

type Field struct {
	Width            uint8         `json:"width"`
	Height           uint8         `json:"height"`
	Position         [2]uint8      `json:"position"`
	OpponentPosition [2]uint8      `json:"opponentPosition"`
	Barriers         [][4][2]uint8 `json:"barriers"`
}

type GameResultInfo struct {
	Result           string        `json:"result"`
	Width            uint8         `json:"width"`
	Height           uint8         `json:"height"`
	Position         [2]uint8      `json:"position"`
	OpponentPosition [2]uint8      `json:"opponentPosition"`
	Barriers         [][4][2]uint8 `json:"barriers"`
}

type PositionStack struct {
	f    [2]uint8
	next *PositionStack
}

func PosPush(stack **PositionStack, position [2]uint8) {
	var newRoot = new(PositionStack)
	*newRoot = PositionStack{
		f:    position,
		next: *stack,
	}
	*stack = newRoot
}

func PosPop(stack **PositionStack) ([2]uint8, bool) {
	if *stack == nil {
		return [2]uint8{}, false
	}
	var temp = *stack
	*stack = (*stack).next
	return temp.f, true
}

package server

type LoginInfo struct {
	Login string `json:"LOGIN"`
}

type Message struct {
	Msg string `json:"MESSAGE"`
}

type LobbyInfo struct {
	ID                 *string `json:"_id"`
	Width              uint8   `json:"width"`
	Height             uint8   `json:"height"`
	GameBarrierCount   uint8   `json:"gameBarrierCount"`
	PlayerBarrierCount uint8   `json:"playerBarrierCount"`
	Name               string  `json:"name"`
	PlayersCount       uint8   `json:"players_count"`
}

type GetLobbyResponse struct {
	Data    []LobbyInfo `json:"DATA"`
	Success bool        `json:"SUCCESS"`
}

type LobbyID struct {
	ID *string `json:"id"`
}

type StartGameInfo struct {
	Move             bool          `json:"move"`
	Width            uint8         `json:"width"`
	Height           uint8         `json:"height"`
	Position         [2]uint8      `json:"position"`
	OpponentPosition [2]uint8      `json:"opponentPosition"`
	Barriers         [][4][2]uint8 `json:"barriers"`
}

type Field struct {
	Width            uint8         `json:"width"`
	Height           uint8         `json:"height"`
	Position         [2]uint8      `json:"position"`
	OpponentPosition [2]uint8      `json:"opponentPosition"`
	Barriers         [][4][2]uint8 `json:"barriers"`
}

type Stats struct {
	Name   string `json:"name"`
	Points uint16 `json:"points"`
}

type JoinLobbyResponse struct {
	Data    LobbyInfo `json:"DATA"`
	Success bool      `json:"SUCCESS"`
}

type EndGameInfo struct {
	Result           string        `json:"result"`
	Width            uint8         `json:"width"`
	Height           uint8         `json:"height"`
	Position         [2]uint8      `json:"position"`
	OpponentPosition [2]uint8      `json:"opponentPosition"`
	Barriers         [][4][2]uint8 `json:"barriers"`
}

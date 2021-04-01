package server

import "errors"

type Lobby struct {
	Info            LobbyInfo
	expectingPlayer *ConnectedClient
	isPlaying       bool
}

func (l Lobby) removePlayer(client *ConnectedClient) {
	if l.isPlaying {
		return
	}
	if l.expectingPlayer == client {
		l.expectingPlayer = nil
	}
}

func (l Lobby) AddPLayer(client *ConnectedClient) error {
	if l.isPlaying {
		return errors.New("trying to join lobby that already started a game")
	}
	if l.expectingPlayer == nil {
		l.expectingPlayer = client
	} else {
		l.isPlaying = true
		go l.startGame(l.expectingPlayer, client)
	}
	return nil
}

func (l Lobby) startGame(player1 *ConnectedClient, player2 *ConnectedClient) {

}

func GetLobby(info LobbyInfo) *Lobby {
	var res = new(Lobby)

	return res
}

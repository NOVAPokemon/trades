package main

import (
	"net/http"
)

func GetCurrentLobbies(w http.ResponseWriter, r *http.Request) {
	HandleGetCurrentLobbies(w, r)
}

func CreateTradeLobby(w http.ResponseWriter, r *http.Request) {
	HandleCreateTradeLobby(w, r)
}

func JoinTradeLobby(w http.ResponseWriter, r *http.Request) {
	HandleJoinTradeLobby(w, r)
}

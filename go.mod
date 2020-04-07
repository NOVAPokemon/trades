module github.com/NOVAPokemon/trades

go 1.13

require (
	github.com/NOVAPokemon/client v0.0.0-20200404150137-d54843f70731 // indirect
	github.com/NOVAPokemon/store v0.0.0-20200402234902-75f7792046b7 // indirect
	github.com/NOVAPokemon/utils v0.0.64
	github.com/gorilla/mux v1.7.4
	github.com/gorilla/websocket v1.4.2
	github.com/sirupsen/logrus v1.5.0
	go.mongodb.org/mongo-driver v1.3.1
)

replace github.com/NOVAPokemon/utils v0.0.64 => ../utils

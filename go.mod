module github.com/NOVAPokemon/trades

go 1.13

require (
	github.com/NOVAPokemon/authentication v0.0.4
	github.com/NOVAPokemon/client v0.0.0-20200326221400-448e50665a63 // indirect
	github.com/NOVAPokemon/trainers v0.0.1
	github.com/NOVAPokemon/utils v0.0.62
	github.com/gorilla/websocket v1.4.2
	github.com/sirupsen/logrus v1.5.0
	go.mongodb.org/mongo-driver v1.3.1
)

replace (
	github.com/NOVAPokemon/trainers v0.0.1 => ../trainers
	github.com/NOVAPokemon/utils v0.0.62 => ../utils
)

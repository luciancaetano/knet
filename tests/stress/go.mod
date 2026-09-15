module github.com/luciancaetano/knet/tests/stress

go 1.26.0

require (
	github.com/gorilla/websocket v1.5.3
	github.com/luciancaetano/knet v0.0.0
)

require (
	github.com/google/uuid v1.6.0 // indirect
	golang.org/x/time v0.16.0 // indirect
)

replace github.com/luciancaetano/knet => ../..

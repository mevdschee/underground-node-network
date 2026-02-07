module github.com/mevdschee/underground-node-network

go 1.24.0

toolchain go1.24.12

require (
	github.com/creack/pty v1.1.24
	github.com/gdamore/tcell/v2 v2.13.7
	github.com/mevdschee/p2pquic-go v0.0.1
	github.com/quic-go/quic-go v0.59.0
	github.com/rivo/uniseg v0.4.7
	golang.org/x/crypto v0.47.0
	golang.org/x/sys v0.40.0
	golang.org/x/term v0.39.0
)

require (
	github.com/gdamore/encoding v1.0.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	golang.org/x/net v0.48.0 // indirect
	golang.org/x/text v0.33.0 // indirect
)

replace github.com/mevdschee/p2pquic-go => ./p2pquic-go

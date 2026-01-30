package ui

import (
	"github.com/mevdschee/underground-node-network/internal/ui/log"
)

type MessageType = log.MessageType

const (
	MsgChat    = log.MsgChat
	MsgSelf    = log.MsgSelf
	MsgCommand = log.MsgCommand
	MsgServer  = log.MsgServer
	MsgSystem  = log.MsgSystem
	MsgAction  = log.MsgAction
	MsgWhisper = log.MsgWhisper
)

type Message = log.Message

type RoomInfo struct {
	Name  string
	Owner string
	Doors []string
}

package log

import (
	"github.com/gdamore/tcell/v2"
	"github.com/mevdschee/underground-node-network/internal/ui/common"
)

type MessageType int

const (
	MsgChat MessageType = iota
	MsgSelf
	MsgCommand
	MsgServer
	MsgSystem
	MsgAction
	MsgWhisper
)

type Message struct {
	Text string
	Type MessageType
}

// LogView manages a scrollable feed of messages
type LogView struct {
	Messages      []Message
	PhysicalLines []Message
	ScrollOffset  int
	Width         int
	lastMsgCount  int
}

func NewLogView() *LogView {
	return &LogView{}
}

func (v *LogView) AddMessage(msg string, msgType MessageType) {
	v.Messages = append(v.Messages, Message{Text: msg, Type: msgType})
}

func (v *LogView) UpdatePhysicalLines(width int) {
	if width == v.Width && len(v.Messages) == v.lastMsgCount && len(v.PhysicalLines) > 0 {
		return
	}
	v.Width = width
	v.lastMsgCount = len(v.Messages)
	v.PhysicalLines = nil
	for _, m := range v.Messages {
		lines := common.WrapText(m.Text, width)
		for _, line := range lines {
			v.PhysicalLines = append(v.PhysicalLines, Message{Text: line, Type: m.Type})
		}
	}
}

func (v *LogView) Draw(s tcell.Screen, x, y, w, h int, baseStyle tcell.Style) {
	if s == nil || h <= 0 {
		return
	}

	v.UpdatePhysicalLines(w)

	totalLines := len(v.PhysicalLines)
	if totalLines == 0 {
		return
	}

	start := totalLines - v.ScrollOffset - h
	if start < 0 {
		start = 0
	}
	end := start + h
	if end > totalLines {
		end = totalLines
	}

	for i, line := range v.PhysicalLines[start:end] {
		style := baseStyle
		switch line.Type {
		case MsgServer:
			style = style.Foreground(tcell.ColorYellow)
		case MsgSystem:
			style = style.Foreground(tcell.ColorDimGray)
		case MsgAction:
			style = style.Foreground(tcell.ColorDarkOrchid)
		case MsgSelf:
			style = style.Foreground(tcell.ColorLightSkyBlue)
		case MsgChat:
			style = style.Foreground(tcell.ColorWhite)
		}
		common.DrawText(s, x, y+i, line.Text, w, style)
	}
}

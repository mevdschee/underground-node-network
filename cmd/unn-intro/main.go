package main

import (
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
)

const (
	tickerRate    = 40 * time.Millisecond
	graphInterval = 150 * time.Millisecond
	charDelayMin  = 5 * time.Millisecond
	charDelayMax  = 15 * time.Millisecond
	blinkRate     = 300 * time.Millisecond
	rainChars     = "0123456789ABCDEF!@#$%^&*()_+-=[]{}|;':,./<>?"
	rainDuration  = 2 * time.Second
)

type State int

const (
	StateRain State = iota
	StateConsole
)

// --- Panel Definitions ---

type Panel struct {
	x, y, w, h int
	title      string
	style      tcell.Style
	mu         sync.Mutex
	lines      []string
}

func (p *Panel) Draw(s tcell.Screen) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for x := p.x; x < p.x+p.w; x++ {
		s.SetContent(x, p.y, tcell.RuneHLine, nil, p.style)
		s.SetContent(x, p.y+p.h-1, tcell.RuneHLine, nil, p.style)
	}
	for y := p.y; y < p.y+p.h; y++ {
		s.SetContent(p.x, y, tcell.RuneVLine, nil, p.style)
		s.SetContent(p.x+p.w-1, y, tcell.RuneVLine, nil, p.style)
	}
	s.SetContent(p.x, p.y, tcell.RuneULCorner, nil, p.style)
	s.SetContent(p.x+p.w-1, p.y, tcell.RuneURCorner, nil, p.style)
	s.SetContent(p.x, p.y+p.h-1, tcell.RuneLLCorner, nil, p.style)
	s.SetContent(p.x+p.w-1, p.y+p.h-1, tcell.RuneLRCorner, nil, p.style)

	titleStr := fmt.Sprintf(" [ %s ] ", p.title)
	for i, r := range titleStr {
		s.SetContent(p.x+2+i, p.y, r, nil, p.style.Bold(true))
	}

	for i, line := range p.lines {
		if i >= p.h-2 {
			break
		}
		for j, r := range line {
			if j >= p.w-4 {
				break
			}
			s.SetContent(p.x+2+j, p.y+1+i, r, nil, p.style)
		}
	}
}

func (p *Panel) AddLine(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lines = append(p.lines, line)
	if len(p.lines) > p.h-2 {
		p.lines = p.lines[1:]
	}
}

func (p *Panel) SetLine(i int, line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for len(p.lines) <= i {
		p.lines = append(p.lines, "")
	}
	p.lines[i] = line
}

type LatencyGraph struct {
	*Panel
	data  []int
	state bool
}

func (g *LatencyGraph) Draw(s tcell.Screen) {
	g.Panel.Draw(s)
	g.mu.Lock()
	defer g.mu.Unlock()

	if len(g.data) == 0 {
		return
	}

	maxVal := 120
	graphH := g.h - 3
	for i, v := range g.data {
		gh := (v * graphH) / maxVal
		if gh < 1 && v > 0 {
			gh = 1
		}
		for j := 0; j < gh; j++ {
			char := '┃'
			if j == gh-1 {
				char = '┏'
			}
			s.SetContent(g.x+2+i, g.y+g.h-2-j, char, nil, g.style)
		}
	}
}

func (g *LatencyGraph) AddPoint(p int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.data = append(g.data, p)
	if len(g.data) > g.w-5 {
		g.data = g.data[1:]
	}
}

type Drop struct {
	x, y   int
	speed  int
	length int
}

// --- Main Helper Functions ---

func drawText(s tcell.Screen, x, y int, text string, style tcell.Style) {
	for i, r := range text {
		s.SetContent(x+i, y, r, nil, style)
	}
}

func typeLine(p *Panel, text string) {
	fullText := text
	for i := 0; i <= len(fullText); i++ {
		p.SetLine(len(p.lines)-1, fullText[:i]+"_")
		time.Sleep(charDelayMin + time.Duration(rand.Intn(int(charDelayMax-charDelayMin))))
	}
	p.SetLine(len(p.lines)-1, fullText)
}

func fillBackground(s tcell.Screen, w, h int, style tcell.Style) {
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			s.SetContent(x, y, ' ', nil, style)
		}
	}
}

func applyGlitch(s tcell.Screen, w, h int) {
	if rand.Float64() < 0.5 {
		row := rand.Intn(h)
		shift := rand.Intn(5) - 2
		for x := 0; x < w; x++ {
			nx := (x + shift + w) % w
			m, c, st, _ := s.GetContent(x, row)
			s.SetContent(nx, row, m, c, st)
		}
	} else {
		for i := 0; i < 50; i++ {
			rx, ry := rand.Intn(w), rand.Intn(h)
			m, c, st, _ := s.GetContent(rx, ry)
			if m != ' ' {
				s.SetContent(rx, ry, rune(rainChars[rand.Intn(len(rainChars))]), c, st.Foreground(tcell.ColorLime))
			}
		}
	}
}

func main() {
	s, err := tcell.NewScreen()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create screen: %v\n", err)
		os.Exit(1)
	}
	if err := s.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to init screen: %v\n", err)
		os.Exit(1)
	}
	defer s.Fini()

	sw, sh := s.Size()
	baseStyle := tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorGreen)
	brightStyle := baseStyle.Foreground(tcell.ColorLime)

	// --- State Management ---
	var mu sync.Mutex
	currentState := StateRain
	rainStartTime := time.Now()

	// --- Component Setup ---
	clockPanel := &Panel{x: sw - 24, y: 1, w: 22, h: 3, title: "SYSTEM_TIME", style: brightStyle}
	scanPanel := &Panel{x: 2, y: 5, w: 30, h: 8, title: "ID_SCANNER", style: baseStyle}
	clientLog := &Panel{x: 34, y: 5, w: (sw - 38) / 2, h: 12, title: "CLIENT_LOG", style: baseStyle}
	serverLog := &Panel{x: 34 + (sw-38)/2 + 2, y: 5, w: (sw - 38) / 2, h: 12, title: "SERVER_LOG", style: brightStyle}
	graphPanel := &LatencyGraph{
		Panel: &Panel{x: 2, y: sh - 9, w: sw - 4, h: 8, title: "P2P_LATENCY_TRACKER (ms)", style: baseStyle},
		data:  make([]int, 0),
	}

	drops := make([]*Drop, sw)
	for i := 0; i < sw; i++ {
		drops[i] = &Drop{x: i, y: rand.Intn(sh), speed: rand.Intn(2) + 1, length: rand.Intn(sh/2) + 5}
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	stopChan := make(chan struct{})

	// --- Background Loops ---

	// Clock Loop
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopChan:
				return
			case <-ticker.C:
				clockPanel.SetLine(0, time.Now().Format("15:04:05.000"))
			}
		}
	}()

	// Graph Loop
	go func() {
		ticker := time.NewTicker(graphInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopChan:
				return
			case <-ticker.C:
				if graphPanel.state {
					p := 20 + rand.Intn(40)
					if rand.Float64() < 0.05 {
						p += 80
					}
					graphPanel.AddPoint(p)
				} else {
					graphPanel.AddPoint(0)
				}
			}
		}
	}()

	// Global Rendering Loop
	go func() {
		ticker := time.NewTicker(tickerRate)
		defer ticker.Stop()
		for {
			select {
			case <-stopChan:
				return
			case <-ticker.C:
				s.Clear()
				mu.Lock()
				st := currentState
				mu.Unlock()

				fillBackground(s, sw, sh, baseStyle)

				if st == StateRain {
					// Draw Rain
					for _, d := range drops {
						d.y += d.speed
						if d.y-d.length > sh {
							d.y = 0
						}
						for i := 0; i < d.length; i++ {
							y := d.y - i
							if y >= 0 && y < sh {
								char := rune(rainChars[rand.Intn(len(rainChars))])
								style := baseStyle
								if i == 0 {
									style = brightStyle.Bold(true)
								} else if i > d.length/2 {
									style = baseStyle.Foreground(tcell.ColorDarkGreen)
								}
								s.SetContent(d.x, y, char, nil, style)
							}
						}
					}
					if rand.Float64() < 0.05 {
						applyGlitch(s, sw, sh)
					}
					if time.Since(rainStartTime) > rainDuration {
						mu.Lock()
						currentState = StateConsole
						mu.Unlock()
					}
				} else {
					// Draw Console
					drawText(s, 2, 1, " [ UNDERGROUND_NODE_NETWORK OPERATOR CONSOLE ] ", brightStyle.Bold(true))
					clockPanel.Draw(s)
					scanPanel.Draw(s)
					clientLog.Draw(s)
					serverLog.Draw(s)
					graphPanel.Draw(s)
					if rand.Float64() < 0.005 {
						applyGlitch(s, sw, sh)
					}
				}
				s.Show()
			}
		}
	}()

	// Scenario Runner
	go func() {
		// Wait for rain to finish
		for {
			mu.Lock()
			st := currentState
			mu.Unlock()
			if st == StateConsole {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		scanPanel.AddLine("STATE: INITIALIZING...")
		time.Sleep(500 * time.Millisecond)
		scanPanel.SetLine(0, "STATE: SCANNING...")
		scanPanel.AddLine("PROBING: ~/.ssh/id_rsa")
		time.Sleep(800 * time.Millisecond)
		scanPanel.AddLine("IDENT: maurits [VERIFIED]")
		scanPanel.AddLine("IP: 192.168.1.157")
		time.Sleep(600 * time.Millisecond)
		scanPanel.AddLine("PROXY: UNN_CONTROL_SUB")
		scanPanel.SetLine(0, "STATE: READY")

		clientLog.AddLine("> BOOTING_UNN_CLIENT...")
		time.Sleep(400 * time.Millisecond)
		clientLog.AddLine("")
		typeLine(clientLog, "> RESOLVING ENTRY_POINT: localhost:44322")

		serverLog.AddLine("> LISTENING ON 0.0.0.0:44322")
		time.Sleep(600 * time.Millisecond)
		serverLog.AddLine("> NEW_CONNECTION [ID: 0x8F2B]")

		clientLog.AddLine("")
		typeLine(clientLog, "> AUTH_HANDSHAKE: SENDING RSA_PUB")
		time.Sleep(300 * time.Millisecond)
		serverLog.AddLine("> AUTH_VERIFIED: USER 'maurits'")

		graphPanel.mu.Lock()
		graphPanel.state = true
		graphPanel.mu.Unlock()

		clientLog.AddLine("")
		typeLine(clientLog, "> REGISTERING ROOM 'lobby'...")
		time.Sleep(700 * time.Millisecond)
		serverLog.AddLine("> ROOM_REG: 'lobby' [ACTIVE]")

		clientLog.AddLine("> STARTING EPHEMERAL SSH SERVER...")
		time.Sleep(500 * time.Millisecond)
		clientLog.AddLine("> BINDING TO PORT 2222...")

		clientLog.AddLine("> NODE_STATUS: ONLINE")
		serverLog.AddLine("> SYNC_COMPLETE. P2P_FABRIC ESTABLISHED.")

		time.Sleep(4 * time.Second)
		close(stopChan)
	}()

	select {
	case <-sigChan:
		close(stopChan)
	case <-stopChan:
	}
}

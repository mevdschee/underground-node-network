package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/mevdschee/underground-node-network/internal/ui/common"
	"github.com/rivo/uniseg"
)

// Default constants for 115200 baud
const (
	baseBaud    = 115200
	baseTicker  = 40 * time.Millisecond
	baseGraph   = 150 * time.Millisecond
	baseCharMin = 5 * time.Millisecond
	baseCharMax = 15 * time.Millisecond
	rainChars   = "0123456789ABCDEF!@#$%^&*()_+-=[]{}|;':,./<>?"
	baseRainDur = 2 * time.Second
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
	visible    bool
	mu         sync.Mutex
	lines      []string
}

func (p *Panel) Draw(s tcell.Screen) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.visible {
		return
	}

	// Opaque background fill: clear out the rain in the panel's area
	for ix := p.x; ix < p.x+p.w; ix++ {
		for iy := p.y; iy < p.y+p.h; iy++ {
			s.SetContent(ix, iy, ' ', nil, tcell.StyleDefault.Background(tcell.ColorBlack))
		}
	}

	// Draw border
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

	// Draw title
	titleStr := fmt.Sprintf(" [ %s ] ", p.title)
	common.DrawText(s, p.x+2, p.y, titleStr, p.w-4, p.style.Bold(true))

	// Draw lines
	for i, line := range p.lines {
		if i >= p.h-2 {
			break
		}
		common.DrawText(s, p.x+2, p.y+1+i, line, p.w-4, p.style)
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
	data   []int
	active bool
}

func (g *LatencyGraph) Draw(s tcell.Screen) {
	if !g.visible {
		return
	}
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
	common.DrawText(s, x, y, text, 1000, style)
}

func typeLine(p *Panel, text string, charMin, charMax time.Duration) {
	fullText := text
	gr := uniseg.NewGraphemes(fullText)
	typed := ""
	for gr.Next() {
		typed += gr.Str()
		p.SetLine(len(p.lines)-1, typed+"_")
		time.Sleep(charMin + time.Duration(rand.Intn(int(charMax-charMin+1))))
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
		for i := 0; i < 30; i++ {
			rx, ry := rand.Intn(w), rand.Intn(h)
			m, c, st, _ := s.GetContent(rx, ry)
			if m != ' ' {
				s.SetContent(rx, ry, rune(rainChars[rand.Intn(len(rainChars))]), c, st.Foreground(tcell.ColorLime))
			}
		}
	}
}

func main() {
	baud := flag.Int("baud", baseBaud, "Simulated baud rate")
	flag.Parse()

	// Calculate scaling factor
	scale := float64(baseBaud) / float64(*baud)
	tickerRate := time.Duration(float64(baseTicker) * scale)
	graphInterval := time.Duration(float64(baseGraph) * scale)
	charDelayMin := time.Duration(float64(baseCharMin) * scale)
	charDelayMax := time.Duration(float64(baseCharMax) * scale)
	rainDuration := time.Duration(float64(baseRainDur) * scale)

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

	// --- Layout Setup ---
	clockPanel := &Panel{x: sw - 24, y: 1, w: 22, h: 3, title: "SYSTEM_TIME", style: brightStyle, visible: false}
	scanPanel := &Panel{x: 2, y: 5, w: 30, h: 8, title: "ID_SCANNER", style: baseStyle, visible: false}
	clientLog := &Panel{x: 34, y: 5, w: (sw - 38) / 2, h: 12, title: "CLIENT_LOG", style: baseStyle, visible: false}
	serverLog := &Panel{x: 34 + (sw-38)/2 + 2, y: 5, w: (sw - 38) / 2, h: 12, title: "SERVER_LOG", style: brightStyle, visible: false}
	graphPanel := &LatencyGraph{
		Panel: &Panel{x: 2, y: sh - 9, w: sw - 4, h: 8, title: "P2P_LATENCY_TRACKER (ms)", style: baseStyle, visible: false},
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
		ticker := time.NewTicker(tickerRate / 2)
		if tickerRate < 20*time.Millisecond {
			ticker = time.NewTicker(10 * time.Millisecond)
		}
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
				if graphPanel.active {
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

	// Render Loop (The Heartbeat)
	go func() {
		ticker := time.NewTicker(tickerRate)
		if tickerRate > 100*time.Millisecond {
			ticker = time.NewTicker(100 * time.Millisecond) // Cap render sleeper
		}
		defer ticker.Stop()

		currentState := StateRain
		rainStartTime := time.Now()

		for {
			select {
			case <-stopChan:
				return
			case <-ticker.C:
				s.Clear()
				fillBackground(s, sw, sh, tcell.StyleDefault.Background(tcell.ColorBlack))

				// Layer 1: Persistent Background Rain
				for _, d := range drops {
					// Scale speed logic: at lower baud, rain is slower
					if rand.Float64() < (1.0 / scale) {
						d.y += d.speed
					}
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
				if rand.Float64() < 0.03 {
					applyGlitch(s, sw, sh)
				}

				// Sequence Control
				if currentState == StateRain && time.Since(rainStartTime) > rainDuration {
					currentState = StateConsole
				}

				// Layer 2: Sequential Console Panels
				if currentState == StateConsole {
					if clockPanel.visible {
						drawText(s, 2, 1, " [ UNDERGROUND_NODE_NETWORK OPERATOR CONSOLE ] ", brightStyle.Bold(true)) // Header appears with clock
						clockPanel.Draw(s)
					}
					scanPanel.Draw(s)
					clientLog.Draw(s)
					serverLog.Draw(s)
					graphPanel.Draw(s)
				}

				s.Show()
			}
		}
	}()

	// --- Scenario Runner (The Script) ---
	go func() {
		defer close(stopChan)

		// Wait for rain to settle/transition
		time.Sleep(rainDuration)

		// 1. Clock and Header reveal
		clockPanel.mu.Lock()
		clockPanel.visible = true
		clockPanel.mu.Unlock()
		time.Sleep(500 * time.Millisecond * time.Duration(scale))

		// 2. Identity Scanner reveal
		scanPanel.mu.Lock()
		scanPanel.visible = true
		scanPanel.mu.Unlock()
		scanPanel.AddLine("STATE: INITIALIZING...")
		time.Sleep(time.Duration(500 * float64(time.Millisecond) * scale))
		scanPanel.SetLine(0, "STATE: SCANNING...")
		scanPanel.AddLine("PROBING: ~/.ssh/id_rsa")
		time.Sleep(time.Duration(800 * float64(time.Millisecond) * scale))
		scanPanel.AddLine("IDENT: maurits [VERIFIED]")
		scanPanel.AddLine("IP: 192.168.1.157")
		time.Sleep(time.Duration(600 * float64(time.Millisecond) * scale))
		scanPanel.AddLine("PROXY: UNN_CONTROL_SUB")
		scanPanel.SetLine(0, "STATE: READY")

		// 3. Client Log reveal
		clientLog.mu.Lock()
		clientLog.visible = true
		clientLog.mu.Unlock()
		clientLog.AddLine("> BOOTING_UNN_CLIENT...")
		time.Sleep(time.Duration(400 * float64(time.Millisecond) * scale))
		clientLog.AddLine("")
		typeLine(clientLog, "> RESOLVING ENTRY_POINT: localhost:44322", charDelayMin, charDelayMax)

		// 4. Server Log reveal
		serverLog.mu.Lock()
		serverLog.visible = true
		serverLog.mu.Unlock()
		serverLog.AddLine("> LISTENING ON 0.0.0.0:44322")
		time.Sleep(time.Duration(600 * float64(time.Millisecond) * scale))
		serverLog.AddLine("> NEW_CONNECTION [ID: 0x8F2B]")

		clientLog.AddLine("")
		typeLine(clientLog, "> AUTH_HANDSHAKE: SENDING RSA_PUB", charDelayMin, charDelayMax)
		time.Sleep(time.Duration(300 * float64(time.Millisecond) * scale))
		serverLog.AddLine("> AUTH_VERIFIED: USER 'maurits'")

		// 5. Graph reveal
		graphPanel.mu.Lock()
		graphPanel.visible = true
		graphPanel.active = true
		graphPanel.mu.Unlock()

		clientLog.AddLine("")
		typeLine(clientLog, "> REGISTERING ROOM 'lobby'...", charDelayMin, charDelayMax)
		time.Sleep(time.Duration(700 * float64(time.Millisecond) * scale))
		serverLog.AddLine("> ROOM_REG: 'lobby' [ACTIVE]")

		clientLog.AddLine("> STARTING EPHEMERAL SSH SERVER...")
		time.Sleep(time.Duration(500 * float64(time.Millisecond) * scale))
		clientLog.AddLine("> BINDING TO PORT 2222...")

		clientLog.AddLine("> NODE_STATUS: ONLINE")
		serverLog.AddLine("> SYNC_COMPLETE. P2P_FABRIC ESTABLISHED.")

		time.Sleep(5 * time.Second)
	}()

	select {
	case <-sigChan:
		close(stopChan)
	case <-stopChan:
	}
}

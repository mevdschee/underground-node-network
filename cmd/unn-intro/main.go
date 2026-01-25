package main

import (
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
)

const (
	rainDuration = 2 * time.Second
	charDelayMin = 4 * time.Millisecond
	charDelayMax = 12 * time.Millisecond
	blinkRate    = 350 * time.Millisecond
	rainChars    = "0123456789ABCDEF!@#$%^&*()_+-=[]{}|;':,./<>?"
)

type Step struct {
	Text  string
	Delay time.Duration
}

var scenario = []Step{
	{"[ SYSTEM ] INITIALIZING UNN CORE: v2.4.0-CLANDESTINE...", 400 * time.Millisecond},
	{"[ NET ] DETECTING LOCAL INTERFACE IP ADDRESS...", 800 * time.Millisecond},
	{"[ NET ] FOUND: 192.168.1.157 (LAN_SECURE)", 300 * time.Millisecond},
	{"[ SEC ] SEARCHING FOR SSH IDENTITY KEYS (~/.ssh/)...", 600 * time.Millisecond},
	{"[ SEC ] FOUND: id_rsa [FPR: SHA256:kN8Xz...v9Q]", 400 * time.Millisecond},
	{"[ HUB ] ATTEMPTING HANDSHAKE WITH ENTRY POINT...", 1000 * time.Millisecond},
	{"[ HUB ] CONNECTED TO: localhost:44322", 300 * time.Millisecond},
	{"[ SYS ] CAPTURING VISITOR PUBLIC KEY FOR P2P HANDOVER...", 600 * time.Millisecond},
	{"[ SEC ] IDENTITY VERIFIED BY ENTRY POINT AS: maurits", 500 * time.Millisecond},
	{"[ NAT ] DISCOVERING NAT CANDIDATES (STUN_PROTOCOL)...", 800 * time.Millisecond},
	{"[ NAT ] STUN_SERVER RESPONSE: 178.239.18.182:54891", 300 * time.Millisecond},
	{"[ LOG ] REGISTERING EPHEMERAL ROOM: 'lobby'...", 500 * time.Millisecond},
	{"[ SYS ] SPINNING UP LOCAL SSH SERVER ON PORT 2222...", 400 * time.Millisecond},
	{"[ MSG ] REGISTRATION COMPLETE. ROOM IS NOW ADVERTISED.", 800 * time.Millisecond},
	{"[ SYS ] UNN NODE IS ONLINE. JACKING IN...", 1500 * time.Millisecond},
}

type Drop struct {
	x, y   int
	speed  int
	length int
}

func main() {
	s, err := tcell.NewScreen()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	if err := s.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	defer s.Fini()

	s.SetStyle(tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorGreen))
	s.Clear()

	w, h := s.Size()
	rand.Seed(time.Now().UnixNano())

	// --- Phase 1: Digital Rain ---
	drops := make([]*Drop, w)
	for i := 0; i < w; i++ {
		drops[i] = &Drop{x: i, y: rand.Intn(h), speed: rand.Intn(2) + 1, length: rand.Intn(h/2) + 5}
	}

	start := time.Now()
	for time.Since(start) < rainDuration {
		s.Clear()
		for _, d := range drops {
			d.y += d.speed
			if d.y-d.length > h {
				d.y = 0
			}
			for i := 0; i < d.length; i++ {
				y := d.y - i
				if y >= 0 && y < h {
					char := rune(rainChars[rand.Intn(len(rainChars))])
					style := tcell.StyleDefault.Background(tcell.ColorBlack).Foreground(tcell.ColorGreen)
					if i == 0 {
						style = style.Foreground(tcell.ColorWhite).Bold(true)
					} else if i > d.length/2 {
						style = style.Foreground(tcell.ColorDarkGreen)
					}
					s.SetContent(d.x, y, char, nil, style)
				}
			}
		}

		// Glitch filter during rain
		if rand.Float64() < 0.05 {
			applyGlitch(s, w, h)
		}

		s.Show()
		time.Sleep(50 * time.Millisecond)
	}

	// --- Phase 2: Technical Scenario ---
	s.Clear()
	styleBase := tcell.StyleDefault.Foreground(tcell.ColorLime)

	for i, step := range scenario {
		row := i + 2
		col := 2

		// Typewriter text
		for _, r := range step.Text {
			s.SetContent(col, row, r, nil, styleBase)
			col += runewidth.RuneWidth(r)
			s.Show()

			// Randomized character delay
			delay := time.Duration(rand.Int63n(int64(charDelayMax-charDelayMin))) + charDelayMin
			time.Sleep(delay)

			// Constant subtle glitch risk
			if rand.Float64() < 0.005 {
				applySubtleGlitch(s, w, h)
				s.Show()
			}
		}

		// BlinkWait
		blinkEnd := time.Now().Add(step.Delay)
		cursorOn := true
		for time.Now().Before(blinkEnd) {
			if cursorOn {
				s.SetContent(col, row, '_', nil, styleBase)
			} else {
				s.SetContent(col, row, ' ', nil, styleBase)
			}
			s.Show()
			cursorOn = !cursorOn
			time.Sleep(blinkRate)
		}
		s.SetContent(col, row, ' ', nil, styleBase) // Clear cursor
		s.Show()
	}

	time.Sleep(1 * time.Second)
}

func applyGlitch(s tcell.Screen, w, h int) {
	// Entire row shift
	if rand.Float64() < 0.5 {
		row := rand.Intn(h)
		shift := rand.Intn(5) - 2
		for x := 0; x < w; x++ {
			nx := (x + shift + w) % w
			m, c, st, _ := s.GetContent(x, row)
			s.SetContent(nx, row, m, c, st)
		}
	} else {
		// Random character swap/corrupt
		for i := 0; i < 50; i++ {
			rx, ry := rand.Intn(w), rand.Intn(h)
			m, c, st, _ := s.GetContent(rx, ry)
			if m != ' ' {
				s.SetContent(rx, ry, rune(rainChars[rand.Intn(len(rainChars))]), c, st.Foreground(tcell.ColorWhite))
			}
		}
	}
}

func applySubtleGlitch(s tcell.Screen, w, h int) {
	rx, ry := rand.Intn(w), rand.Intn(h)
	m, c, st, _ := s.GetContent(rx, ry)
	if m != ' ' {
		s.SetContent(rx, ry, m, c, st.Foreground(tcell.ColorRed))
	}
}

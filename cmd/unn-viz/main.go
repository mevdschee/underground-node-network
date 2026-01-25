package main

import (
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/term"
)

const (
	tickerRate   = 50 * time.Millisecond
	rainDuration = 5 * time.Second
	chars        = "0123456789ABCDEF!@#$%^&*()_+-=[]{}|;':,./<>?"
)

type State int

const (
	StateRain State = iota
	StateTransition
	StateLogo
)

var logoLines = []string{
	`  _   _ _  _ ___  ____ ____ ____ ____ ____ _  _ _  _ ___  `,
	`  |   | |\ | |  \ |___ |__/ | __ |__/ |  | |  | |\ | |  \ `,
	`  |___| | \| |__/ |___ |  \ |__] |  \ |__| |__| | \| |__/ `,
	``,
	`              _  _ ____ ___  ____ `,
	`              |\ | |  | |  \ |___ `,
	`              | \| |__| |__/ |___ `,
	``,
	`      _  _ ____ ___ _  _ ____ ____ _  _ `,
	`      |\ | |___  |  |  | |  | |__/ |_/  `,
	`      | \| |___  |  \__/ |__| |  \ | \  `,
}

var logoColors = []string{
	"\033[1;35m", // Magenta
	"\033[1;36m", // Cyan
	"\033[1;33m", // Yellow
}

type Drop struct {
	x, y   int
	speed  int
	length int
}

type Viz struct {
	width, height int
	state         State
	startTime     time.Time
	drops         []*Drop
}

func NewViz(w, h int) *Viz {
	drops := make([]*Drop, w)
	for i := 0; i < w; i++ {
		drops[i] = &Drop{
			x:      i,
			y:      rand.Intn(h),
			speed:  rand.Intn(2) + 1,
			length: rand.Intn(h/2) + 5,
		}
	}

	return &Viz{
		width:     w,
		height:    h,
		state:     StateRain,
		startTime: time.Now(),
		drops:     drops,
	}
}

func (v *Viz) Update() {
	elapsed := time.Since(v.startTime)

	if v.state == StateRain && elapsed > rainDuration {
		v.state = StateTransition
	}

	if v.state == StateRain {
		for _, d := range v.drops {
			d.y += d.speed
			if d.y-d.length > v.height {
				d.y = 0
				d.length = rand.Intn(v.height/2) + 5
			}
		}
	}
}

func (v *Viz) Draw() {
	if v.state == StateRain {
		v.drawRain()
	} else if v.state == StateTransition {
		fmt.Print("\033[H\033[J") // Clear screen
		v.state = StateLogo
	} else if v.state == StateLogo {
		v.drawLogo()
	}
}

func (v *Viz) drawRain() {
	// We don't clear the screen fully to keep the "trails" feel if we used it,
	// but here we clear everything to match the request "green on black" strictly.
	fmt.Print("\033[H\033[J")

	for _, d := range v.drops {
		for i := 0; i < d.length; i++ {
			y := d.y - i
			if y >= 0 && y < v.height {
				char := chars[rand.Intn(len(chars))]
				if i == 0 {
					// Bright head
					fmt.Printf("\033[%d;%dH\033[1;37m%c\033[0m", y+1, d.x+1, char)
				} else {
					// Fading tail
					opacity := 2 // Persistent green
					if i > d.length/2 {
						opacity = 22 // Dimmer green (faint)
					}
					fmt.Printf("\033[%d;%dH\033[%d;32m%c\033[0m", y+1, d.x+1, opacity, char)
				}
			}
		}
	}
}

func (v *Viz) drawLogo() {
	// Re-draw logo centered
	logoH := len(logoLines)
	logoW := 0
	for _, l := range logoLines {
		if len(l) > logoW {
			logoW = len(l)
		}
	}

	startY := (v.height - logoH) / 2
	startX := (v.width - logoW) / 2

	if startY < 0 {
		startY = 0
	}
	if startX < 0 {
		startX = 0
	}

	for i, line := range logoLines {
		color := logoColors[i/4%len(logoColors)]
		fmt.Printf("\033[%d;%dH%s%s\033[0m", startY+i+1, startX+1, color, line)
	}
}

func main() {
	// Set terminal to raw mode
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err == nil {
		defer term.Restore(int(os.Stdin.Fd()), oldState)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Hide cursor
	fmt.Print("\033[?25l")
	defer fmt.Print("\033[?25h\033[H\033[J")

	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		w, h = 80, 24
	}

	viz := NewViz(w, h)
	ticker := time.NewTicker(tickerRate)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if curW, curH, err := term.GetSize(int(os.Stdout.Fd())); err == nil && (curW != w || curH != h) {
				w, h = curW, curH
				viz.width = w
				viz.height = h
				// Recenter logic will handle it during Draw
			}
			viz.Update()
			viz.Draw()
		case <-sigChan:
			return
		}
	}
}

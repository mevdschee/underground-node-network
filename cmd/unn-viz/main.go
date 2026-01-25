package main

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/term"
)

const (
	tickerRate = 50 * time.Millisecond
	nodeCount  = 8
	linkProb   = 0.4
)

type Node struct {
	Name   string
	X, Y   float64
	VX, VY float64
}

type Link struct {
	A, B *Node
}

type Viz struct {
	width, height int
	nodes         []*Node
	links         []Link
	startTime     time.Time
}

func NewViz(w, h int) *Viz {
	names := []string{"XENON", "NEON", "ARGON", "KRYPTON", "RADON", "HELIUM", "PROXY", "VOID"}
	nodes := make([]*Node, nodeCount)
	for i := 0; i < nodeCount; i++ {
		nodes[i] = &Node{
			Name: names[i%len(names)],
			X:    rand.Float64() * float64(w),
			Y:    rand.Float64() * float64(h),
			VX:   (rand.Float64() - 0.5) * 2,
			VY:   (rand.Float64() - 0.5) * 2,
		}
	}

	links := []Link{}
	for i := 0; i < nodeCount; i++ {
		for j := i + 1; j < nodeCount; j++ {
			if rand.Float64() < linkProb {
				links = append(links, Link{A: nodes[i], B: nodes[j]})
			}
		}
	}

	return &Viz{
		width:     w,
		height:    h,
		nodes:     nodes,
		links:     links,
		startTime: time.Now(),
	}
}

func (v *Viz) Update() {
	for _, n := range v.nodes {
		// Update position
		n.X += n.VX
		n.Y += n.VY

		// Bounce off walls
		if n.X < 2 || n.X > float64(v.width)-15 {
			n.VX *= -1
		}
		if n.Y < 2 || n.Y > float64(v.height)-2 {
			n.VY *= -1
		}

		// Add some jitter
		n.VX += (rand.Float64() - 0.5) * 0.1
		n.VY += (rand.Float64() - 0.5) * 0.1

		// Limit speed
		n.VX = math.Max(-1.5, math.Min(1.5, n.VX))
		n.VY = math.Max(-1.5, math.Min(1.5, n.VY))
	}
}

func (v *Viz) Draw() {
	// Clear screen and move cursor to top-left
	fmt.Print("\033[H\033[J")

	// Draw links first (background)
	t := time.Since(v.startTime).Seconds()
	for _, l := range v.links {
		v.drawLink(l.A.X, l.A.Y, l.B.X, l.B.Y, t)
	}

	// Draw nodes (foreground)
	for _, n := range v.nodes {
		v.drawNode(n.X, n.Y, n.Name)
	}

	// Add some decorative noise
	if rand.Float64() < 0.05 {
		v.drawNoise()
	}
}

func (v *Viz) drawNode(x, y float64, name string) {
	ix, iy := int(x), int(y)
	if ix < 0 || iy < 0 || ix >= v.width || iy >= v.height {
		return
	}
	// Glowing green phosphor
	fmt.Printf("\033[%d;%dH\033[1;36m▚ \033[1;32m%s \033[1;36m▞\033[0m", iy, ix, name)
}

func (v *Viz) drawLink(x1, y1, x2, y2 float64, t float64) {
	// Simple character-based line drawing
	steps := 15
	dx := (x2 - x1) / float64(steps)
	dy := (y2 - y1) / float64(steps)

	for i := 0; i <= steps; i++ {
		px := x1 + float64(i)*dx
		py := y1 + float64(i)*dy
		ix, iy := int(px), int(py)

		if ix >= 0 && iy >= 0 && ix < v.width && iy < v.height {
			// Pulsing intensity based on time and position
			pulse := math.Sin(t*3 + float64(i)*0.5)
			if pulse > 0.5 {
				fmt.Printf("\033[%d;%dH\033[2;32m⠿\033[0m", iy, ix)
			} else if pulse > -0.5 {
				fmt.Printf("\033[%d;%dH\033[2;32m⠶\033[0m", iy, ix)
			} else {
				fmt.Printf("\033[%d;%dH\033[2;32m.\033[0m", iy, ix)
			}
		}
	}
}

func (v *Viz) drawNoise() {
	x := rand.Intn(v.width)
	y := rand.Intn(v.height)
	codes := []string{"0xAF", "0x2D", "0xFE", "SYS_INT", "PKT_LOST"}
	code := codes[rand.Intn(len(codes))]
	fmt.Printf("\033[%d;%dH\033[2;32m%s\033[0m", y, x, code)
}

func main() {
	// Set terminal to raw mode to handle input and suppress echo
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err == nil {
		defer term.Restore(int(os.Stdin.Fd()), oldState)
	}

	// Trap exit signal
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
			// Re-check size occasionally
			if curW, curH, err := term.GetSize(int(os.Stdout.Fd())); err == nil && (curW != w || curH != h) {
				w, h = curW, curH
				viz.width = w
				viz.height = h
			}
			viz.Update()
			viz.Draw()
		case <-sigChan:
			return
		}
	}
}

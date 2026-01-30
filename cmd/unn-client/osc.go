package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/mevdschee/underground-node-network/internal/protocol"
	"golang.org/x/term"
)

var ansiRegex = regexp.MustCompile(`\x1b(\[([0-9;]*[a-zA-Z])|c|\[\?[0-9]+[hl])`)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

func handleOSCPopup(p protocol.PopupPayload) {
	// 1. Clear screen and move to top
	fmt.Print("\033[H\033[2J")

	// 2. Measure terminal
	termWidth, termHeight := 80, 24
	if w, h, err := term.GetSize(int(os.Stdin.Fd())); err == nil {
		termWidth, termHeight = w, h
	}

	// 3. Prepare box content
	lines := strings.Split(p.Message, "\n")
	contentWidth := len(p.Title) + 4
	for _, l := range lines {
		if len(l) > contentWidth {
			contentWidth = len(l)
		}
	}

	boxWidth := contentWidth + 6
	if boxWidth > termWidth-8 {
		boxWidth = termWidth - 8
	}
	if boxWidth < 30 {
		boxWidth = 30
	}

	boxHeight := len(lines) + 4

	// 4. Calculate offsets for centering
	topOffset := (termHeight - boxHeight) / 2
	leftPaddingStr := strings.Repeat(" ", (termWidth-boxWidth)/2)

	// 5. Advanced Colors (256-color fallback)
	borderColor := "38;5;39" // Information blue
	icon := "ⓘ"
	if p.Type == "error" {
		borderColor = "38;5;196" // Error red
		icon = "✖"
	} else if p.Type == "warning" {
		borderColor = "38;5;214" // Warning orange
		icon = "⚠"
	}

	titleColor := "\033[1;48;5;235;38;5;255m" // Dark bg, white text
	primary := "\033[" + borderColor + "m"
	shadow := "\033[38;5;240m"
	reset := "\033[0m"

	// 6. Print top offset
	fmt.Print(strings.Repeat("\r\n", topOffset))

	// 7. Print Box with Shadow
	// Header line
	fmt.Printf("%s%s▛%s▜%s\r\n", leftPaddingStr, primary, strings.Repeat("▀", boxWidth-2), reset)

	// Title line
	titleText := fmt.Sprintf("%s %s", icon, strings.ToUpper(p.Title))
	tPadLeft := (boxWidth - 4 - len(titleText)) / 2
	tPadRight := boxWidth - 4 - len(titleText) - tPadLeft
	fmt.Printf("%s%s▌ %s%s%s%s %s▐%s█%s\r\n", leftPaddingStr, primary, titleColor, strings.Repeat(" ", tPadLeft), titleText, strings.Repeat(" ", tPadRight), reset+primary, shadow, reset)

	// Separator
	fmt.Printf("%s%s▙%s▟%s█%s\r\n", leftPaddingStr, primary, strings.Repeat("▄", boxWidth-2), shadow, reset)

	// Message body
	for _, l := range lines {
		if len(l) > boxWidth-6 {
			l = l[:boxWidth-9] + "..."
		}
		lPadLeft := (boxWidth - 4 - len(l)) / 2
		lPadRight := boxWidth - 4 - len(l) - lPadLeft
		fmt.Printf("%s%s▌ %s%s%s %s▐%s█%s\r\n", leftPaddingStr, primary, strings.Repeat(" ", lPadLeft), l, strings.Repeat(" ", lPadRight), primary, shadow, reset)
	}

	// Bottom border
	fmt.Printf("%s%s▙%s▟%s█%s\r\n", leftPaddingStr, primary, strings.Repeat("▀", boxWidth-2), shadow, reset)
	fmt.Printf("%s  %s%s%s\r\n", leftPaddingStr, shadow, strings.Repeat("▀", boxWidth-1), reset)

	// 8. Space for prompt
	fmt.Print("\r\n\r\n")
}

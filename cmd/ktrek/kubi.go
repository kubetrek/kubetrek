package main

import (
	"fmt"
	"os"
	"strings"
)

type mood int

const (
	happy mood = iota
	worried
)

// Each letter is one pixel; two pixel rows fit in one terminal row using ▀ (top = foreground, bottom = background).
// M and N are the happy and worried mouths.
var kubiPixels = []string{
	"..C.....C..",
	"...G...G...",
	"..WWWWWWW..",
	".WVVVVVVVW.",
	"GWVEVVVEVWG",
	"GWVVMMMVVWG",
	".WVVNNNVVW.",
	"..WWWWWWW..",
	"...WBBBW...",
	"...WBBBW...",
	"....WWW....",
	".....C.....",
}

var palette = map[byte][3]int{
	'W': {238, 243, 250},
	'G': {159, 178, 204},
	'V': {11, 23, 48},
	'B': {50, 108, 229},
	'C': {95, 243, 255},
}

var eyes = map[mood][3]int{
	happy:   {95, 243, 255},
	worried: {255, 181, 71},
}

func pixel(m mood, c byte) (rgb [3]int, ok bool) {
	switch {
	case c == 'E', c == 'M' && m == happy, c == 'N' && m == worried:
		return eyes[m], true
	case c == 'M', c == 'N':
		return palette['V'], true
	}
	rgb, ok = palette[c]
	return rgb, ok
}

func kubiArt(m mood) []string {
	var rows []string
	for i := 0; i < len(kubiPixels); i += 2 {
		var b strings.Builder
		for x := range len(kubiPixels[i]) {
			top, hasTop := pixel(m, kubiPixels[i][x])
			bot, hasBot := pixel(m, kubiPixels[i+1][x])
			switch {
			case !hasTop && !hasBot:
				b.WriteByte(' ')
			case !hasTop:
				fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm▄\x1b[0m", bot[0], bot[1], bot[2])
			case !hasBot:
				fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm▀\x1b[0m", top[0], top[1], top[2])
			default:
				fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm▀\x1b[0m", top[0], top[1], top[2], bot[0], bot[1], bot[2])
			}
		}
		rows = append(rows, b.String())
	}
	return rows
}

func colorful() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0 && os.Getenv("NO_COLOR") == ""
}

// kubi draws Kubi with the lines next to it, starting on the second row so the text sits by the face.
func kubi(m mood, lines ...string) {
	if !colorful() {
		for _, l := range lines {
			fmt.Println(l)
		}
		return
	}
	fmt.Println()
	for i, row := range kubiArt(m) {
		text := ""
		if i > 0 && i-1 < len(lines) {
			text = lines[i-1]
		}
		fmt.Printf(" %s   %s\n", row, text)
	}
	fmt.Println()
}

func style(code string) func(string) string {
	return func(s string) string {
		if !colorful() {
			return s
		}
		return "\x1b[" + code + "m" + s + "\x1b[0m"
	}
}

var (
	bold  = style("1")
	cyan  = style("38;2;95;243;255")
	blue  = style("38;2;110;155;255")
	amber = style("38;2;255;181;71")
	green = style("38;2;120;225;150")
	gray  = style("38;2;135;148;168")
)

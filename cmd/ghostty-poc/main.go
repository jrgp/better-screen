package main

import (
	"fmt"
	"log"
	"os"

	"github.com/jrgp/bs/internal/vt"
)

func main() {
	t, err := vt.New(40, 8)
	if err != nil {
		log.Fatal(err)
	}
	defer t.Close()
	t.VTWrite([]byte("primary\x1b[?1049h\x1b[2J\x1b[2;3H\x1b[1;38;2;255;80;0mGhostty 世界\x1b[0m\x1b[4;6H"))
	if err := t.Resize(45, 10, 0, 0); err != nil {
		log.Fatal(err)
	}
	f, err := vt.Capture(t, 45, 10, 0)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := fmt.Fprint(os.Stdout, vt.Draw(nil, f)); err != nil {
		log.Fatal(err)
	}
}

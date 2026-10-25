package main

import (
	"os"

	"github.com/jrgp/bs/internal/mux"
)

func main() { os.Exit(mux.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

package main

import (
	"os"

	"github.com/jrgp/bs/internal/mux"
)

// Set at link time for releases; application dependencies receive it explicitly.
var version = mux.Version

func main() { os.Exit(mux.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, version)) }

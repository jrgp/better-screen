package mux

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"slices"
	"strings"
	"syscall"
	"time"
)

const Version = "0.1.0"

func Main(args []string, in, out *os.File, stderr io.Writer) int {
	if err := run(args, in, out, stderr); err != nil {
		fmt.Fprintf(stderr, "bs: %v\n", err)
		return 1
	}
	return 0
}

func run(args []string, in, out *os.File, stderr io.Writer) error {
	if len(args) == 1 && args[0] == "__daemon" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		signal.Ignore(syscall.SIGHUP)
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/sh"
		}
		return NewServer(home, shell, log.New(stderr, "bs daemon: ", log.LstdFlags)).Run(ctx)
	}
	flags := flag.NewFlagSet("bs", flag.ContinueOnError)
	flags.SetOutput(stderr)
	name := flags.String("S", "", "create a named session")
	reattach := flags.String("r", "", "reattach to a session")
	multi := flags.String("x", "", "attach without disconnecting other clients (name optional for a sole session)")
	detach := flags.Bool("d", false, "detach existing clients (with -r)")
	list := flags.Bool("ls", false, "list sessions")
	version := flags.Bool("v", false, "print version")
	// flag has no optional string arguments; spell an omitted name as -x=.
	args = slices.Clone(args)
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			break
		}
		switch args[i] {
		case "-S", "--S", "-r", "--r":
			i++
		case "-x", "--x":
			if i+1 == len(args) || strings.HasPrefix(args[i+1], "-") {
				args[i] += "="
			} else {
				i++
			}
		}
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if *version {
		fmt.Fprintf(out, "bs %s\n", Version)
		return nil
	}
	multiRequested := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "x" {
			multiRequested = true
		}
	})
	selected := 0
	for _, active := range []bool{*name != "", *reattach != "", multiRequested, *list} {
		if active {
			selected++
		}
	}
	if selected > 1 || (*detach && *reattach == "") {
		return fmt.Errorf("use bs [-S name | -ls | -r name | -x [name] | -d -r name]")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	r := request{Version: protocolVersion, Op: "create", Name: *name, Force: *detach}
	if *reattach != "" {
		r.Op, r.Name = "attach", *reattach
	}
	if multiRequested {
		r.Op, r.Name, r.Multi = "attach", *multi, true
	}
	start := r.Op == "create" && !*list
	conn, err := connect(home, start)
	if err != nil {
		if !start && (errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED)) {
			if *list {
				fmt.Fprintln(out, "No sessions.")
				return nil
			}
			return fmt.Errorf("no session matching %q", r.Name)
		}
		return err
	}
	defer conn.Close()
	if *list {
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		r.Op = "list"
		if err := send(conn, r); err != nil {
			return err
		}
		var result response
		if err := receive(conn, &result, maxMessage); err != nil {
			return err
		}
		if result.Version != protocolVersion {
			return fmt.Errorf("incompatible daemon protocol")
		}
		if result.Error != "" {
			return errors.New(result.Error)
		}
		if len(result.Sessions) == 0 {
			fmt.Fprintln(out, "No sessions.")
			return nil
		}
		for _, info := range result.Sessions {
			state := "detached"
			if info.Attached > 0 {
				state = fmt.Sprintf("%d attached", info.Attached)
			}
			fmt.Fprintf(out, "%s\t%d windows\t%s\n", info.Name, info.Windows, state)
		}
		return nil
	}
	return attach(conn, in, out, r)
}

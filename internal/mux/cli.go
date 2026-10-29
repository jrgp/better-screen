package mux

import (
	"cmp"
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

func Main(args []string, in, out *os.File, stderr io.Writer, buildVersion string) int {
	if err := run(args, in, out, stderr, buildVersion); err != nil {
		fmt.Fprintf(stderr, "bs: %v\n", err)
		return 1
	}
	return 0
}

func run(args []string, in, out *os.File, stderr io.Writer, buildVersion string) error {
	if len(args) == 1 && args[0] == "__daemon" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		config, err := loadConfig(home)
		if err != nil {
			return err
		}
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		signal.Ignore(syscall.SIGHUP)
		shell := cmp.Or(os.Getenv("SHELL"), "/bin/sh")
		return NewServer(home, shell, log.New(stderr, "bs daemon: ", log.LstdFlags), config).Run(ctx)
	}
	flags := flag.NewFlagSet("bs", flag.ContinueOnError)
	flags.SetOutput(stderr)
	name := flags.String("S", "", "create a named session")
	reattach := flags.String("r", "", "reattach to a session")
	multi := flags.String("x", "", "attach without disconnecting other clients (name optional for a sole session)")
	detach := flags.Bool("d", false, "detach existing clients (with -r)")
	list := flags.Bool("ls", false, "list sessions")
	version := flags.Bool("v", false, "print version")
	panes := flags.Bool("panes", false, "run each following command in its own pane")
	var commands []string
	// flag has no optional string arguments; spell an omitted name as -x=.
	args = slices.Clone(args)
scan:
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			break
		}
		switch args[i] {
		case "--panes", "-panes":
			commands, args = args[i+1:], args[:i+1]
			break scan
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
	if *panes {
		incompatible := false
		flags.Visit(func(f *flag.Flag) {
			if f.Name != "S" && f.Name != "panes" {
				incompatible = true
			}
		})
		if incompatible {
			return fmt.Errorf("--panes may only be combined with -S")
		}
		if len(commands) == 0 {
			return fmt.Errorf("--panes requires at least one command")
		}
		if err := validatePaneCommands(commands); err != nil {
			return err
		}
	}
	if *version {
		fmt.Fprintf(out, "bs %s\n", buildVersion)
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
	if _, err := loadConfig(home); err != nil {
		return err
	}
	r := request{Version: protocolVersion, Op: opCreate, Name: *name, Force: *detach, Commands: commands}
	if *reattach != "" {
		r.Op, r.Name = opAttach, *reattach
	}
	if multiRequested {
		r.Op, r.Name, r.Multi = opAttach, *multi, true
	}
	start := r.Op == opCreate && !*list
	if start {
		r.Dir, err = os.Getwd()
		if errors.Is(err, os.ErrNotExist) {
			r.Dir = home
		} else if err != nil {
			return fmt.Errorf("determine session directory: %w", err)
		}
	}
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
		r.Op = opList
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

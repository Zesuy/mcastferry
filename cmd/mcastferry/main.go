package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"

	"github.com/zesuy/mcastferry/internal/config"
)

var version = "dev"

func main() {
	os.Exit(run(os.Stdout, os.Stderr, os.Args[1:]))
}

func run(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("mcastferry", flag.ContinueOnError)
	fs.SetOutput(stderr)
	showVersion := fs.Bool("version", false, "print version and exit")
	options := config.BindFlags(fs)
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mcastferry [options]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "mcastferry %s\n", version)
		return 0
	}
	_, err := options.Resolve(func(name string) (int, error) {
		iface, lookupErr := net.InterfaceByName(name)
		if lookupErr != nil {
			return 0, lookupErr
		}
		return iface.Index, nil
	})
	if err != nil {
		fmt.Fprintf(stderr, "configuration error: %v\n", err)
		return 2
	}
	fmt.Fprintln(stderr, "mcastferry relay runtime is not wired yet")
	return 1
}

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/forsyth/dbm"
)

func main() {
	flag.Parse()
	if flag.NArg() < 2 {
		fmt.Fprint(os.Stderr, "Usage: dbm/delete dbmfile key\n")
		os.Exit(2)
	}
	db, err := dbm.Open(flag.Arg(0))
	if err != nil {
		fatal("cannot open %s: %s", flag.Arg(0), err)
	}
	key := flag.Arg(1)
	if err := db.Delete([]byte(key)); err != nil {
		fatal("cannot delete %s: %s", key, err)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", os.Args[0], fmt.Sprintf(format, args...))
	os.Exit(1)
}

package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/forsyth/dbm"
)

func main() {
	flag.Parse()
	if na := flag.NArg(); na == 0 || (na-1)%2 != 0 {
		fmt.Fprint(os.Stderr, "Usage: dbm/store dbmfile [key value] ...\n")
		os.Exit(2)
	}
	db, err := dbm.Open(flag.Arg(0))
	if err != nil {
		fatal("cannot open %s: %s", flag.Arg(0), err)
	}
	if flag.NArg() < 2 {
		nerr := 0
		f := bufio.NewScanner(os.Stdin)
		for f.Scan() {
			s := f.Text()
			key, val, err := kvLine(s)
			if err != nil {
				fmt.Fprintf(os.Stderr, "dbm/store: %s\n", err)
				nerr++
				continue
			}
			if s == "" || s == "\n" || s[0] == '#' {
				continue
			}
			err = store(db, key, val)
			if err != nil {
				fmt.Fprintf(os.Stderr, "dbm/store: %s: %s", key, err)
				nerr++
			}
		}
		err = f.Err()
		if err != nil {
			fatal("dbm/store: error reading standard input: %s", err)
		}
		if nerr != 0 {
			os.Exit(1)
		}
	} else {
		err := store(db, flag.Arg(1), flag.Arg(2))
		if err != nil {
			fatal("store %s: %s", flag.Arg(0), err)
		}
	}
}

func kvLine(s string) (string, string, error) {
	if s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}
	i := strings.IndexAny(s, " \t")
	if i < 0 {
		return "", "", fmt.Errorf("bad input: %q", s)
	}
	return s[0:i], s[i+1:], nil
}

func store(db *dbm.File, key, data string) error {
	err := db.Store([]byte(key), []byte(data), false)
	if err != nil {
		if errors.Is(err, dbm.ErrDuplicate) {
			fmt.Fprintf(os.Stderr, "dbm/store: key %q exists\n", key)
			return nil
		}
		fmt.Fprintf(os.Stderr, "dbm/store: key %q: %s\n", key, err)
		return err
	}
	return nil
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", os.Args[0], fmt.Sprintf(format, args...))
	os.Exit(1)
}

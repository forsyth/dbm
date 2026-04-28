package main

import (
	"bufio"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"unicode"
	"unicode/utf8"

	"github.com/forsyth/dbm"
)

func main() {
	flag.Parse()
	if flag.NArg() < 2 {
		fmt.Fprint(os.Stderr, "Usage: dbm/fetch dbmfile key ...\n")
		os.Exit(2)
	}
	db, err := dbm.Open(flag.Arg(0))
	if err != nil {
		fatal("cannot open %s: %s", flag.Arg(0), err)
	}
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	for _, key := range flag.Args()[1:] {
		d, err := db.Fetch([]byte(key))
		if err != nil {
			fatal("error fetching key %s: %s", key, err)
		}
		out.WriteString(fmt.Sprintf("%s\t%s\n", key, display(d)))
	}
}

func display(v []byte) string {
	for i := 0; i < len(v); {
		r, w := utf8.DecodeRune(v[i:])
		if !unicode.IsGraphic(r) { // covers RuneError too
			return hex.EncodeToString(v)
		}
		i += w
	}
	return string(v)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s: %s\n", os.Args[0], fmt.Sprintf(format, args...))
	os.Exit(1)
}

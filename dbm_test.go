package dbm_test

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/forsyth/dbm"
)

const (
	wordsFile = "testdata/words"
)

func linesReader(f io.Reader, line func(lno int, key string, err error)) {
	lines := bufio.NewScanner(f)
	for lno := 1; lines.Scan(); lno++ {
		key := strings.TrimSpace(lines.Text())
		line(lno, key, nil)
	}
}

func TestDBM(t *testing.T) {
	// load
	words, err := os.Open(wordsFile)
	if err != nil {
		t.Fatalf("cannot open %s: %s", wordsFile, err)
	}
	defer words.Close()
	db, err := dbm.Create("testdata/db1")
	if err != nil {
		t.Fatalf("cannot create testdata/db1: %s", err)
	}
	defer db.Close()
	vals := make(map[string][]byte)
	val := []byte{}
	linesReader(words, func(lno int, key string, err error) {
		if len(key) == 0 {
			t.Errorf("line %d: empty key", lno)
			return
		}
		val = append(val, 'a')
		if len(val) > 200 {
			val = []byte{}
		}
		err = db.Store([]byte(key), val, false)
		if err != nil {
			t.Errorf("line %d: adding key %q (data len %d), want success; got %s", lno, key, len(val), err)
			return
		}
		gb, err := db.Fetch([]byte(key))
		if err != nil {
			t.Errorf("line %d: refetch key %q: want success; got %s", lno, key, err)
			return
		}
		if !bytes.Equal(gb, val) {
			t.Errorf("line %d: refetch key %q: want %q; got %q", lno, key, val, gb)
		}
		vals[key] = val
	})
	t.Logf("LOAD OK")
	_, err = words.Seek(0, io.SeekStart)
	if err != nil {
		t.Fatalf("cannot rewind %s: %s", wordsFile, err)
		return
	}
	linesReader(words, func(lno int, key string, err error) {
		gb, err := db.Fetch([]byte(key))
		if err != nil {
			t.Errorf("key %q fetch want success; got %s", key, err)
			return
		}
		if !bytes.Equal(gb, vals[key]) {
			t.Errorf("key %q fetch want %q; got %q", key, vals[key], gb)
		}
	})
}

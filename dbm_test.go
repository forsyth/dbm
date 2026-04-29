package dbm_test

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/forsyth/dbm"
)

const (
	wordsFile = "testdata/words"
)

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
	lines := bufio.NewScanner(words)
	val := []byte{}
	for lines.Scan() {
		key := strings.TrimSpace(lines.Text())
		val = append(val, 'a')
		if len(val) > 200 {
			val = []byte{}
		}
		err := db.Store([]byte(key), val, false)
		if err != nil {
			t.Errorf("adding key %s (data len %d), want success; got %s", key, len(val), err)
		}
		fmt.Printf("%s\n", key)
	}
}

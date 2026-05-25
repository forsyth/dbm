// Package dbm provides a small, simple key/value store implemented
// using an extensible hash function.
// It is a Go version of the original.
// A USENIX paper [Seltzer & Yigit] discusses all the dbm/ndbm/sdbm variants,
// including the extensible hash variants.
//
// [Seltzer & Yigit:] https://www.usenix.org/legacy/publications/library/proceedings/seltzer2.pdf
package dbm

// Original C code Copyright © AT&T 1979 (Ken Thompson)
// Limbo transliteration (with amendment) Copyright © 2004 Vita Nuova Holdings Limited
// Go version Copyright © 2024 Charles Forsyth (charles.forsyth@gmail.com)

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

const (
	byteBits   = 8
	shortBytes = 2

	pageBlockSize = 512 // tiny for this century
	dirBlockSize  = 8192
)

var (
	ErrBadOpen        = errors.New("bad open file flag")
	ErrCorrupt        = errors.New("corrupt data block")
	ErrDuplicate      = errors.New("key already exists")
	ErrNotFound       = errors.New("key not found")
	ErrNotPaired      = errors.New("items not in pairs")
	ErrReadOnly       = errors.New("read-only file")
	ErrSplitNotPaired = errors.New("split found key/value not paired")
	ErrTooLarge       = errors.New("key/value too large")
)

type hash uint32

// File represents an open database instance.
type File struct {
	dirf      *os.File // directory file
	pagf      *os.File // page file
	writable  bool
	maxbno    int64 // last `bno' in page file
	bitno     int64
	hmask     hash
	blkno     int64 // current page to read/write
	pageBlkno int64 // current page in pageBuf
	pageBuf   []byte
	dirBlkno  int64 // current block in dirBuf
	dirBuf    []byte
	ovfBuf    []byte // block with pairs split from pageBuf
}

// Create makes a new dbm database, with the given name as the base name,
// and returns a [File] that can access it.
// If the database already exists, it is truncated.
// Currently a database has two underlying storage files for a given name N: N.pag and N.dat.
func Create(name string) (*File, error) {
	pf, err := os.Create(name + ".pag")
	if err != nil {
		return nil, fmt.Errorf("cannot create %s.pag: %w", name, err)
	}
	df, err := os.Create(name + ".dir")
	if err != nil {
		return nil, fmt.Errorf("cannot create %s.dir: %w", name, err)
	}
	return alloc(pf, df, true)
}

// Open opens the dbm file for reading and writing.
func Open(name string) (*File, error) {
	return OpenFile(name, os.O_RDWR, 0o666)
}

// OpenFile opens the dbm file in the mode given by [flag]:
// [os.O_RDONLY] for only reading, or [os.O_RDWR] for reading and writing (updating).
// Neither [os.O_WRONLY] nor [os.O_APPEND] are allowed in a [flag].
// If a file does not exist and [os.O_CREATE] creates it, the file permissions
// are set to [perms].
// An error is returned if any underlying OS files cannot be opened.
func OpenFile(name string, flag int, perms os.FileMode) (*File, error) {
	if (flag&(os.O_RDONLY|os.O_WRONLY|os.O_RDWR)) == os.O_WRONLY ||
		(flag&os.O_APPEND) != 0 {
		return nil, ErrBadOpen
	}
	pf, err := os.OpenFile(name+".pag", flag, perms)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s.pag: %w", name, err)
	}
	df, err := os.OpenFile(name+".dir", flag, perms)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s.dir: %w", name, err)
	}
	return alloc(pf, df, flag != os.O_RDONLY)
}

func alloc(pf *os.File, df *os.File, writable bool) (*File, error) {
	d, err := pf.Stat()
	if err != nil {
		return nil, fmt.Errorf("dbm cannot get file size: %w", err)
	}
	db := &File{
		pagf:      pf,
		dirf:      df,
		writable:  writable,
		pageBuf:   make([]byte, pageBlockSize),
		dirBuf:    make([]byte, dirBlockSize),
		ovfBuf:    make([]byte, pageBlockSize),
		pageBlkno: -1,
		dirBlkno:  -1,
		maxbno:    d.Size()*byteBits - 1,
	}
	return db, nil
}

// Reset discards the in-memory cache.
func (db *File) Reset() {
	db.pageBlkno = -1
	db.dirBlkno = -1
}

// Flush flushes any cached but unwritten data and clears the caches.
func (db *File) Flush() {
	// nothing volatile to flush, but discard cache
	db.Reset()
}

// Close flushes any cached data and closes the underlying OS files.
func (db *File) Close() {
	db.Flush()
	db.dirf.Close()
	db.pagf.Close()
}

// IsReadOnly reports whether the file was opened only for reading and cannot be updated.
func (db *File) IsReadOnly() bool {
	return !db.writable
}

// Fetch returns the data associated with the given key,
// or nil if key does not exist.
// If an error is returned, the database is in bad shape.
func (db *File) Fetch(key []byte) ([]byte, error) {
	err := db.access(calcHash(key))
	if err != nil {
		return nil, err
	}
	for i := 0; ; i += 2 {
		item := mkItem(db.pageBuf, i)
		if item == nil {
			return nil, nil
		}
		if cmpDatum(key, item) == 0 {
			item = mkItem(db.pageBuf, i+1)
			if item == nil {
				return nil, ErrNotPaired
			}
			return item, nil
		}
	}
}

// Delete deletes the data associated with key, or returns error ErrNotFound.
// If any other error is returned, the database is read-only or in bad shape.
func (db *File) Delete(key []byte) error {
	if db.IsReadOnly() {
		return ErrReadOnly
	}
	err := db.access(calcHash(key))
	if err != nil {
		return err
	}
	for i := 0; ; i += 2 {
		item := mkItem(db.pageBuf, i)
		if item == nil {
			return ErrNotFound
		}
		if cmpDatum(key, item) == 0 {
			delItem(db.pageBuf, i)
			delItem(db.pageBuf, i)
			break
		}
	}
	db.pageBlkno = db.blkno
	return writeBlock(db.pagf, db.pageBuf, db.blkno)
}

// RecordFits returns true iff the given key and data can be stored in the database.
func (db *File) RecordFits(key []byte, dat []byte) bool {
	// using < not <= for compatibility with the original.
	return len(key)+len(dat)+3*shortBytes < pageBlockSize
}

// Store stores the (key, data) pair and returns nil (no error) on success.
// If replace is true and key already has an associated value, Store replaces that by the new data;
// otherwise it returns error ErrDuplicate.
func (db *File) Store(key []byte, data []byte, replace bool) error {
	if db.IsReadOnly() {
		return ErrReadOnly
	}
	if !db.RecordFits(key, data) {
		return ErrTooLarge
	}
	hash := calcHash(key)
	for {
		err := db.access(hash)
		if err != nil {
			return err
		}
		for i := 0; ; i += 2 {
			item := mkItem(db.pageBuf, i)
			if item == nil {
				break
			}
			if cmpDatum(key, item) == 0 {
				if !replace {
					return ErrDuplicate
				}
				delItem(db.pageBuf, i)
				delItem(db.pageBuf, i)
				break
			}
		}
		// key and data must be on the same page
		i := addItem(db.pageBuf, key)
		if i >= 0 {
			if addItem(db.pageBuf, data) >= 0 {
				break
			}
			delItem(db.pageBuf, i)
		}
		if err := db.split(); err != nil {
			return err
		}
	}
	db.pageBlkno = db.blkno
	return writeBlock(db.pagf, db.pageBuf, db.blkno)
}

// split splits the current block to try to make space for a new
// key, value pair. It extends the hash function by one bit, and
// moves the pairs that new bit covers into the new block,
// adding the bit to the hash directory.
func (db *File) split() error {
	clear(db.ovfBuf)
	for i := 0; ; {
		item := mkItem(db.pageBuf, i)
		if item == nil {
			break
		}
		if (calcHash(item) & (db.hmask + 1)) == 0 {
			// new hash bit not set, doesn't move
			i += 2
			continue
		}
		// shunt key/value pairs selected by new hash from current page to the overflow page
		addItem(db.ovfBuf, item)
		delItem(db.pageBuf, i)
		item = mkItem(db.pageBuf, i)
		if item == nil {
			return ErrSplitNotPaired
		}
		addItem(db.ovfBuf, item)
		delItem(db.pageBuf, i)
	}
	err := writeBlock(db.pagf, db.pageBuf, db.blkno)
	if err != nil {
		return err
	}
	db.pageBlkno = db.blkno
	err = writeBlock(db.pagf, db.ovfBuf, db.blkno+int64(db.hmask+1))
	if err != nil {
		return err
	}
	db.setBit()
	return nil
}

// FirstKey returns the value of the first key in the database
// in an internal database ordering, to start iterating over the set of keys.
func (db *File) FirstKey() ([]byte, error) {
	key, err := db.firstHash(0)
	if err != nil {
		return nil, err
	}
	return bytes.Clone(key), nil
}

// NextKey returns the value of the key immediately following the given one,
// to continue iterating over the set of keys.
func (db *File) NextKey(key []byte) ([]byte, error) {
	hash := calcHash(key)
	err := db.access(hash)
	if err != nil {
		return nil, err
	}
	var item, bitem []byte
	for i := 0; ; i += 2 {
		item = mkItem(db.pageBuf, i)
		if item == nil {
			break
		}
		if cmpDatum(key, item) <= 0 {
			continue
		}
		if bitem == nil || cmpDatum(bitem, item) < 0 {
			bitem = item
		}
	}
	if bitem != nil {
		return bytes.Clone(bitem), nil
	}
	hash = db.hashInc(hash)
	if hash == 0 {
		return bytes.Clone(item), nil
	}
	fh, err := db.firstHash(hash)
	if err != nil {
		return nil, err
	}
	return bytes.Clone(fh), nil
}

func (db *File) firstHash(hash hash) ([]byte, error) {
	for {
		err := db.access(hash)
		if err != nil {
			return nil, err
		}
		bitem := mkItem(db.pageBuf, 0)
		var item []byte
		for i := 2; ; i += 2 {
			item = mkItem(db.pageBuf, i)
			if item == nil {
				break
			}
			if cmpDatum(bitem, item) < 0 {
				bitem = item
			}
		}
		if bitem != nil {
			return bitem, nil
		}
		hash = db.hashInc(hash)
		if hash == 0 {
			return item, nil
		}
	}
}

// access, given a [hash] will load the bucket that has
// the data for that hash value, or an error if there
// is an IO error or inconsistency.
func (db *File) access(hash hash) error {
	for db.hmask = 0; ; db.hmask = (db.hmask << 1) + 1 {
		db.blkno = int64(hash & db.hmask)
		db.bitno = db.blkno + int64(db.hmask)
		b, err := db.getBit()
		if err != nil {
			return err
		}
		if b == 0 {
			// not split, stop
			break
		}
	}
	if db.blkno != db.pageBlkno {
		err := readBlock(db.pagf, db.pageBuf, db.blkno)
		if err != nil {
			return err
		}
		err = checkBlock(db.pageBuf)
		if err != nil {
			return err
		}
		db.pageBlkno = db.blkno
	}
	return nil
}

func (db *File) getBit() (byte, error) {
	if db.bitno > db.maxbno {
		return 0, nil
	}
	n := db.bitno % byteBits
	bn := db.bitno / byteBits
	i := bn % dirBlockSize
	b := bn / dirBlockSize
	if b != db.dirBlkno {
		err := readBlock(db.dirf, db.dirBuf, b)
		if err != nil {
			return 0, err
		}
		db.dirBlkno = b
	}
	return db.dirBuf[i] & (1 << n), nil
}

func (db *File) setBit() error {
	if db.bitno > db.maxbno {
		db.maxbno = db.bitno
		_, err := db.getBit()
		if err != nil {
			return err
		}
	}
	n := db.bitno % byteBits
	bn := db.bitno / byteBits
	i := bn % dirBlockSize
	b := bn / dirBlockSize
	db.dirBuf[i] |= byte(1 << n)
	db.dirBlkno = b
	return writeBlock(db.dirf, db.dirBuf, b)
}

// mkItem returns a slice of [buf] referring to the
// [n]'th item in the bucket (either key or value),
// returning nil if there is no item for the index [n].
func mkItem(buf []byte, n int) []byte {
	ne := get16(buf, 0)
	if n < 0 || n >= ne {
		return nil
	}
	t := pageBlockSize
	if n > 0 {
		t = get16(buf, n+1-1)
	}
	v := get16(buf, n+1)
	return buf[v:t]
}

func cmpDatum(d1 []byte, d2 []byte) int {
	n := len(d1)
	if n != len(d2) {
		return n - len(d2)
	}
	if n == 0 {
		return 0
	}
	return bytes.Compare(d1, d2)
}

// ken's
//
//	055,043,036,054,063,014,004,005,
//	010,064,077,000,035,027,025,071,
//

var hitab = [16]hash{
	61, 57, 53, 49, 45, 41, 37, 33,
	29, 25, 21, 17, 13, 9, 5, 1,
}

var hltab = [64]hash{
	0o6100151277, 0o6106161736, 0o6452611562, 0o5001724107,
	0o2614772546, 0o4120731531, 0o4665262210, 0o7347467531,
	0o6735253126, 0o6042345173, 0o3072226605, 0o1464164730,
	0o3247435524, 0o7652510057, 0o1546775256, 0o5714532133,
	0o6173260402, 0o7517101630, 0o2431460343, 0o1743245566,
	0o0261675137, 0o2433103631, 0o3421772437, 0o4447707466,
	0o4435620103, 0o3757017115, 0o3641531772, 0o6767633246,
	0o2673230344, 0o0260612216, 0o4133454451, 0o0615531516,
	0o6137717526, 0o2574116560, 0o2304023373, 0o7061702261,
	0o5153031405, 0o5322056705, 0o7401116734, 0o6552375715,
	0o6165233473, 0o5311063631, 0o1212221723, 0o1052267235,
	0o6000615237, 0o1075222665, 0o6330216006, 0o4402355630,
	0o1451177262, 0o2000133436, 0o6025467062, 0o7121076461,
	0o3123433522, 0o1010635225, 0o1716177066, 0o5161746527,
	0o1736635071, 0o6243505026, 0o3637211610, 0o1756474365,
	0o4723077174, 0o3642763134, 0o5750130273, 0o3655541561,
}

func (db *File) hashInc(hash hash) hash {
	hash &= db.hmask
	bit := db.hmask + 1
	for {
		bit >>= 1
		if bit == 0 {
			return 0
		}
		if (hash & bit) == 0 {
			return hash | bit
		}
		hash &= ^bit
	}
}

// calcHash returns the hash value for the given [item], any slice of bytes.
// As the USENIX paper observes, this ``bit-randomizing'' hash function
// ``is important to obtain radically different hash values for nearly identical keys,
// which in turn avoids clustering of such keys in a single bucket''.
func calcHash(item []byte) hash {
	hashl := hash(0)
	hashi := hash(0)
	for i := 0; i < len(item); i++ {
		f := int(item[i])
		for j := 0; j < byteBits; j += 4 {
			hashi += hitab[f&0xF]
			hashl += hltab[hashi&0x3F]
			f >>= 4
		}
	}
	return hashl
}

// delItem removes the [n]'th item from the bucket
// [buf], returning an error if the bucket is corrupt.
func delItem(buf []byte, n int) error {
	ne := get16(buf, 0)
	if n < 0 || n >= ne {
		return ErrCorrupt
	}
	i1 := get16(buf, n+1)
	i2 := pageBlockSize
	if n > 0 {
		i2 = get16(buf, n+1-1)
	}
	i3 := get16(buf, ne+1-1)
	if i2 > i1 {
		if d := i1 - i3; d > 0 {
			i2 -= d
			copy(buf[i2:], buf[i3:i1])
			clear(buf[i3:i2])
			i1 -= d
		}
	}
	i2 -= i1
	for i1 = n + 1; i1 < ne; i1++ {
		put16(buf, i1+1-1, get16(buf, i1+1)+i2)
	}
	put16(buf, 0, ne-1)
	put16(buf, ne, 0)
	return nil
}

// addItem adds an [item] to the bucket [buf],
// returning the resulting number of entries
// (the index of the next entry).
// It returns -1 if the item will not fit.
func addItem(buf []byte, item []byte) int {
	i1 := pageBlockSize
	ne := get16(buf, 0)
	if ne > 0 {
		i1 = get16(buf, ne+1-1)
	}
	i1 -= len(item)
	i2 := (ne + 2) * shortBytes
	if i1 <= i2 {
		// not enough space for key and value
		// index entries
		return -1
	}
	put16(buf, ne+1, i1)
	copy(buf[i1:], item)
	put16(buf, 0, ne+1)
	return ne
}

// checkBlock applies some rudimentary consistency
// checks on the index section of the bucket,
// returning an error if it is corrupt.
func checkBlock(buf []byte) error {
	t := pageBlockSize
	ne := get16(buf, 0)
	for i := 0; i < ne; i++ {
		v := get16(buf, i+1)
		if v > t {
			return ErrCorrupt
		}
		t = v
	}
	if t < (ne+1)*shortBytes {
		return ErrCorrupt
	}
	return nil
}

func readBlock(fd *os.File, buf []byte, blk int64) error {
	n := len(buf)
	nr, err := fd.ReadAt(buf, blk*int64(n))
	if err != nil {
		if err == io.EOF && nr == 0 {
			clear(buf)
			return nil
		}
		return fmt.Errorf("dbm read error, block %d: %w", blk, err)
	}
	return nil
}

func writeBlock(fd *os.File, buf []byte, blk int64) error {
	_, err := fd.WriteAt(buf, blk*int64(len(buf)))
	if err != nil {
		return fmt.Errorf("dbm write error, block %d: %w", blk, err)
	}
	return nil
}

func get16(buf []byte, sh int) int {
	sh *= shortBytes
	return int(buf[sh])<<8 | int(buf[sh+1])
}

func put16(buf []byte, sh int, v int) {
	sh *= shortBytes
	buf[sh] = byte(v >> 8)
	buf[sh+1] = byte(v)
}

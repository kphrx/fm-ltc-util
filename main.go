package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"os"
)

type Options struct {
	InputFile string
}

func optParse(args []string) *Options {
	opts := Options{}

	flagSet := flag.NewFlagSet("fm-ltc-util", flag.ExitOnError)
	flagSet.StringVar(&opts.InputFile, "input-file", "", "input file")
	flagSet.Parse(os.Args[1:])

	return &opts
}

const (
	SEEK_START   = 0
	SEEK_CURRENT = 1
	SEEK_END     = 2
)

type LTCHeader struct {
	ID      uint16
	Format  string
	Version uint16
}

func readHeader(f *os.File) (*LTCHeader, error) {
	ib := make([]byte, 2)
	fb := make([]byte, 4)
	vb := make([]byte, 2)
	if _, err := f.Read(ib); err != nil {
		return nil, err
	}
	if _, err := f.Read(fb); err != nil {
		return nil, err
	}
	if _, err := f.Read(vb); err != nil {
		return nil, err
	}

	h := LTCHeader{
		ID:      binary.LittleEndian.Uint16(ib[:2]),
		Format:  string(fb[:4]),
		Version: binary.LittleEndian.Uint16(vb[:2]),
	}

	return &h, nil
}

type LTCRecordsHeader struct {
	Start       int64
	Size        int64
	EndPosition int64

	LangName string
}

func readRecordsHeader(f *os.File) (*LTCRecordsHeader, error) {
	start, err := f.Seek(0, SEEK_CURRENT)
	if err != nil {
		return nil, err
	}

	sb := make([]byte, 8)
	if _, err := f.Read(sb); err != nil {
		return nil, err
	}
	// drop 4 bytes
	size := int64(binary.LittleEndian.Uint32(sb[:4]))

	lb := make([]byte, 4)
	if _, err := f.Read(lb); err != nil {
		return nil, err
	}

	nb := make([]byte, binary.LittleEndian.Uint32(lb[:4]))
	nn, err := f.Read(nb)
	if err != nil {
		return nil, err
	}

	h := LTCRecordsHeader{
		Start:       start,
		Size:        size,
		EndPosition: size + start,
		LangName:    string(nb[:nn]),
	}

	return &h, nil
}

type LTCRecord struct {
	Prefix int16
	Text   string
}

func readRecord(f *os.File) (*LTCRecord, error) {
	rl := make([]byte, 5)
	if _, err := f.Read(rl); err != nil {
		return nil, err
	}

	var (
		p int16 = -1
		l uint32
	)
	if rl[4] == 0 {
		p = int16(rl[0])
		l = binary.LittleEndian.Uint32(rl[1:])
	} else {
		l = binary.LittleEndian.Uint32(rl[:4])
		if _, err := f.Seek(-1, SEEK_CURRENT); err != nil {
			return nil, err
		}
	}

	b := make([]byte, l)
	n, err := f.Read(b)
	if err != nil {
		return nil, err
	}

	r := LTCRecord{
		Prefix: p,
		Text:   string(b[:n]),
	}

	return &r, nil
}

func readRecords(f *os.File, eor int64) error {
	stat := make(map[int16]int)
	var (
		lastPrefix   int16
		lastPosition int64
		lastValue    string
		last         uint = 0
	)

	for {
		record, err := readRecord(f)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			fmt.Printf("Last value count: %d\n", last)
			fmt.Printf("Last value prefix: %d\n\n", lastPrefix)

			if _, err := f.Seek(lastPosition, SEEK_START); err != nil {
				return err
			}
			fmt.Printf("Current offset: %d\n", lastPosition)

			u2 := make([]byte, 8)
			if _, err := f.Read(u2); err != nil {
				return err
			}
			return fmt.Errorf("Unknown bytes: 0x%x\n", u2)
		}

		co, err := f.Seek(0, SEEK_CURRENT)
		if err != nil {
			return err
		}

		stat[record.Prefix] += 1
		lastPosition = co
		lastPrefix = record.Prefix
		lastValue = record.Text
		last += 1

		if co >= eor {
			fmt.Printf("Value prefix stats: %v\n", stat)
			break
		}
	}

	fmt.Printf("Last value of %d: %s (%d)\n", last, lastValue, lastPrefix)

	return nil
}

type LTCKey struct {
	Flag byte
	ID     uint32
	Offset uint32
}

func getKeys15(f *os.File) ([]LTCKey, error) {
	b := make([]byte, 4)
	if _, err := f.Read(b); err != nil {
		return nil, err
	}

	c := binary.LittleEndian.Uint32(b)

	ks := make([]LTCKey, c)
	for i := range c {
		kb := make([]byte, 4)
		if _, err := f.Read(kb); err != nil {
			return nil, err
		}

		vb := make([]byte, 4)
		if _, err := f.Read(vb); err != nil {
			return nil, err
		}

		sb := make([]byte, 1)
		if _, err := f.Read(sb); err != nil {
			return nil, err
		}

		ks[i] = LTCKey{
			Flag: sb[0],
			ID: binary.LittleEndian.Uint32(kb[:4]),
			Offset: binary.LittleEndian.Uint32(vb[:4]),
		}
	}

	return ks, nil
}

func getKeys16(f *os.File) ([]LTCKey, error) {
	b := make([]byte, 4)
	if _, err := f.Read(b); err != nil {
		return nil, err
	}

	c := binary.LittleEndian.Uint32(b)

	ks := make([]LTCKey, c)
	for i := range c {
		kb := make([]byte, 4)
		if _, err := f.Read(kb); err != nil {
			return nil, err
		}

		vb := make([]byte, 4)
		if _, err := f.Read(vb); err != nil {
			return nil, err
		}

		ks[i] = LTCKey{
			Flag: kb[3],
			ID: binary.LittleEndian.Uint32(kb[:4]) & 0x00ffffff,
			Offset: binary.LittleEndian.Uint32(vb[:4]),
		}
	}

	return ks, nil
}

func getKeys(f *os.File, v uint16, eor int64) ([]LTCKey, error) {
	if _, err := f.Seek(eor, SEEK_START); err != nil {
		return nil, err
	}

	switch v {
	case 15:
		return getKeys15(f)
	case 16:
		return getKeys16(f)
	default:
		return nil, fmt.Errorf("unsupported LTC version: %d", v)
	}
}

func check(e error) {
	if e != nil {
		panic(e)
	}
}

func main() {
	opts := optParse(os.Args)

	f, err := os.Open(opts.InputFile)
	check(err)
	defer f.Close()

	h, err := readHeader(f)
	check(err)
	fmt.Printf("ID:\t\t%d\n", h.ID)
	fmt.Printf("Format:\t\t%s\n", h.Format)
	fmt.Printf("Version:\t%d\n", h.Version)

	header, err := readRecordsHeader(f)
	check(err)
	fmt.Printf("Size:\t\t%d\n", header.Size)
	fmt.Printf("EndPosition:\t%d\n", header.EndPosition)
	fmt.Printf("LangName:\t%s\n", header.LangName)

	u1 := make([]byte, 4)
	_, err = f.Read(u1)
	check(err)
	fmt.Printf("Unknown bytes:\t0x%x\n", u1)

	// err = readRecords(f, header.EndPosition)
	// check(err)

	keys, err := getKeys(f, h.Version, header.EndPosition)
	check(err)

	l := len(keys)
	key := keys[l-1]

	_, err = f.Seek(int64(key.Offset) + header.Start, SEEK_START)
	check(err)

	record, err := readRecord(f)
	check(err)
	fmt.Printf("Last value: %s (%d)\n", record.Text, record.Prefix)

	fmt.Printf("Key of %d:", l)
	fmt.Printf("\tFlag: 0x%x, ID: %d (0x%x), Offset: %d (0x%x)\n", key.Flag, key.ID, key.ID, key.Offset, key.Offset)
}

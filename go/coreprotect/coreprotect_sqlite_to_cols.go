package main

import (
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"

	lz4 "github.com/DataDog/golz4-2"
	_ "github.com/mattn/go-sqlite3"
)

func iabs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

func imax(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func compress(buf []byte) []byte {
	comp := make([]byte, lz4.CompressBoundHdr(buf)+1)
	len, err := lz4.CompressHCHdr(comp[1:], buf)
	if err != nil {
		panic(err)
	}
	//comp := make([]byte, snappy.MaxEncodedLen(len(buf))+1)
	//ret := snappy.Encode(comp[1:], buf)
	//return comp[:len(ret)+1]
	return comp[:len+1]
}

func decompress(buf []byte) []byte {
	decomp, err := lz4.UncompressAllocHdr(nil, buf)
	if err != nil {
		panic(err)
	}
	return decomp
}

func comp(run []int) []byte {
	// try a few different encodings. return the best one.
	// methods:
	//  0: 8-bit + lz4
	//  1: varint + lz4,
	//  2: delta-varint + lz4
	//  3: delta-delta-varint + lz4
	buf := make([]byte, len(run)*5)

	best := []byte{}

	// try just doing varints
	off := 0
	for _, x := range run {
		off += binary.PutVarint(buf[off:], int64(x))
	}
	enc := compress(buf[:off])
	enc[0] = 1
	best = enc

	// delta-varint
	off = 0
	last := 0
	for _, x := range run {
		x, last = x-last, x
		off += binary.PutVarint(buf[off:], int64(x))
	}
	enc = compress(buf[:off])
	enc[0] = 2
	if len(enc) < len(best) {
		best = enc
	}

	// some data is trivially 8-bit
	canByteEncode := true
	for _, x := range run {
		if x > 255 || x < 0 {
			canByteEncode = false
			break
		}
	}
	if canByteEncode {
		for i, x := range run {
			buf[i] = byte(x)
		}
		enc = compress(buf[:len(run)])
		enc[0] = 0
		if len(enc) < len(best) {
			best = enc
		}
	}

	return best
}

func decomp(comp []byte) []int {
	buf := decompress(comp[1:])
	//  0: 8-bit + lz4
	//  1: varint + lz4,
	//  2: delta-varint + lz4
	ret := []int{}
	switch comp[0] {
	case 0:
		for _, c := range buf {
			ret = append(ret, int(c))
		}
	case 1:
		for i := 0; i < len(buf); {
			val, len := binary.Varint(buf[i:])
			i += len
			ret = append(ret, int(val))
		}
	case 2:
		last := 0
		for i := 0; i < len(buf); {
			val, len := binary.Varint(buf[i:])
			i += len
			ret = append(ret, int(val)+last)
			last += int(val)
		}
	}
	return ret

}

func compMeta(metas [][]byte) []byte {
	minLen := 0
	for _, meta := range metas {
		minLen += 4 + len(meta)
	}

	buf := make([]byte, minLen)
	off := 0
	for _, meta := range metas {
		off += binary.PutVarint(buf[off:], int64(len(meta)))
		if len(meta) > 0 {
			copy(buf[off:off+len(meta)], meta)
			off += len(meta)
		}
	}
	return compress(buf[:off])[1:]
}

func decompMeta(comp []byte) [][]byte {
	ret := [][]byte{}
	buf := decompress(comp)
	for i := 0; i < len(buf); {
		val, len := binary.Varint(buf[i:])
		i += len
		if val == 0 {
			ret = append(ret, nil)
		} else {
			ret = append(ret, buf[i:i+int(val)])
		}
		i += int(val)
	}
	return ret
}

func compressBufs(bufs [][]int, metas [][]byte, extras ...int) ([][]byte, int, []byte) {
	headerBuf := make([]byte, (len(bufs)+len(extras)+1)*5)
	headerOff := 0
	if len(bufs) > 0 {
		headerOff += binary.PutVarint(headerBuf[headerOff:], int64(len(bufs[0])))
	} else {

		headerOff += binary.PutVarint(headerBuf[headerOff:], int64(len(metas)))
	}
	for _, extra := range extras {
		headerOff += binary.PutVarint(headerBuf[headerOff:], int64(extra))
	}

	clen := 0
	var cbufs [][]byte
	for _, buf := range bufs {
		cbuf := comp(buf)
		cbufs = append(cbufs, cbuf)
		clen += len(cbuf)
		dbuf := decomp(cbuf)
		if !reflect.DeepEqual(buf, dbuf) {
			fmt.Println(buf[:20])
			fmt.Println(dbuf[:20])
			panic("roundtrip failed! D:")
		}
	}
	if metas != nil {
		metaComp := compMeta(metas)
		metasRoundTrip := decompMeta(metaComp)
		if !reflect.DeepEqual(metas, metasRoundTrip) {
			fmt.Println(len(metas), len(metasRoundTrip), metas[:20], metasRoundTrip[:20])
			panic("metas roundtrip failed! D:")
		}
		cbufs = append(cbufs, metaComp)
		clen += len(compMeta(metas))
	}

	for _, cbuf := range cbufs {
		headerOff += binary.PutVarint(headerBuf[headerOff:], int64(len(cbuf)))
	}
	return cbufs, clen, headerBuf[:headerOff]
}

func dumpBlocks(db *sql.DB, fout, fidx *os.File) {
	// CREATE TABLE co_block (time INTEGER, user INTEGER, wid INTEGER, x INTEGER, y INTEGER, z INTEGER, type INTEGER, data INTEGER, meta BLOB, action INTEGER, rolled_back INTEGER);
	// requires: CREATE INDEX block_region_index on co_block(wid,x>>9,z>>9);
	stmt, err := db.Prepare("SELECT * from co_block where wid=? and x>>9=? and z>>9=?")
	if err != nil {
		log.Fatal(err)
	}
	// sqlite3 -csv database.db 'select distinct wid, x>>9, z>>9 from co_block' > co_block_regions
	regionBuf, err := ioutil.ReadFile("co_block_regions")
	if err != nil {
		log.Fatal(err)
	}
	lines := strings.Split(string(regionBuf), "\n")
	regions := [][]int{}

	for _, line := range lines {
		vals := strings.Split(line, ",")
		if len(vals) < 3 {
			break
		}
		rWid, _ := strconv.Atoi(vals[0])
		rx, _ := strconv.Atoi(vals[1])
		rz, _ := strconv.Atoi(vals[2])
		regions = append(regions, []int{rWid, rx, rz})
	}
	sort.Slice(regions, func(i, j int) bool {
		if regions[i][0] != regions[j][0] {
			return regions[i][0] < regions[j][0]
		}
		arx, brx, arz, brz := regions[i][1], regions[j][1], regions[i][2], regions[j][2]
		if true {
			distA := imax(iabs(arx), iabs(arz))
			distB := imax(iabs(brx), iabs(brz))
			if distA != distB {
				return distA < distB
			}
		}
		if arx != brx {
			return arx < brx
		}
		return arz < brz
	})
	// regions = regions[4000:]
	total := 0

	for n, region := range regions {
		rWid, rx, rz := region[0], region[1], region[2]
		// rows, err := stmt.Query(rWid, rx<<9, (rx+1)<<9-1, rz<<9, (rz+1)<<9-1)
		rows, err := stmt.Query(rWid, rx, rz)
		if err != nil {
			log.Fatal(err)
		}
		for more := true; more; {
			var time, user, wid, x, y, z, ty, data, action, rolledback int
			var meta []byte
			var bufs [10][]int
			var metas [][]byte
			more = false
			for rows.Next() {
				rows.Scan(&time, &user, &wid, &x, &y, &z, &ty, &data, &meta, &action, &rolledback)
				//fmt.Println(x>>9, z>>9, time, user, wid, x, y, z, data, ty, data, meta, action, rolledback)
				bufs[0] = append(bufs[0], time)
				bufs[1] = append(bufs[1], user)
				bufs[2] = append(bufs[2], wid)
				bufs[3] = append(bufs[3], x)
				bufs[4] = append(bufs[4], y)
				bufs[5] = append(bufs[5], z)
				bufs[6] = append(bufs[6], ty)
				bufs[7] = append(bufs[7], data)
				bufs[8] = append(bufs[8], action)
				bufs[9] = append(bufs[9], rolledback)
				metas = append(metas, meta)
				if len(bufs[0])%100000 == 0 {
					fmt.Println(rx, rz, len(bufs[0]))
				}
				if len(bufs[0])%10000000 == 0 {
					more = true
					break
				}
			}

			cbufs, clen, header := compressBufs(bufs[:], metas)
			total += clen

			fmt.Printf("%d/%d %d %d %d => %d %dKB  ", n+1, len(regions), rWid, rx, rz, len(bufs[0]), clen/1024)
			for i, cbuf := range cbufs[:10] {
				fmt.Printf("%s%d:%d ", "Tuwxyztdar"[i:i+1], cbuf[0], len(cbuf)/1024)
			}
			fmt.Printf("m:%d\n", len(cbufs[len(cbufs)-1])/1024)

			write(fout, fidx, header, cbufs)
		}
	}
	fmt.Printf("blocks total MB: %0.2fMB\n", float64(total)/1024/1024)
}

func dumpContainers(db *sql.DB, fout, fidx *os.File) {
	// CREATE TABLE co_container (time INTEGER, user INTEGER, wid INTEGER, x INTEGER, y INTEGER, z INTEGER, type INTEGER, data INTEGER, amount INTEGER, metadata BLOB, action INTEGER, rolled_back INTEGER);
	rows, err := db.Query("select * from co_container")
	if err != nil {
		log.Fatal(err)
	}
	var time, user, wid, x, y, z, ty, data, amount, action, rolledback int
	var meta []byte
	var bufs [11][]int
	var metas [][]byte
	for rows.Next() {
		rows.Scan(&time, &user, &wid, &x, &y, &z, &ty, &data, &amount, &meta, &action, &rolledback)
		bufs[0] = append(bufs[0], time)
		bufs[1] = append(bufs[1], user)
		bufs[2] = append(bufs[2], wid)
		bufs[3] = append(bufs[3], x)
		bufs[4] = append(bufs[4], y)
		bufs[5] = append(bufs[5], z)
		bufs[6] = append(bufs[6], ty)
		bufs[7] = append(bufs[7], data)
		bufs[8] = append(bufs[8], amount)
		bufs[9] = append(bufs[9], action)
		bufs[10] = append(bufs[10], rolledback)
		metas = append(metas, meta)
		if len(bufs[0])%100000 == 0 {
			fmt.Println("container", len(bufs[0]))
		}
	}
	fmt.Println("container", len(bufs[0]))

	cbufs, clen, header := compressBufs(bufs[:], metas)

	fmt.Printf("%d %dKB  ", len(bufs[0]), clen/1024)
	for i, cbuf := range cbufs[:11] {
		fmt.Printf("%s%d:%d ", "Tuwxyztdamar"[i:i+1], cbuf[0], len(cbuf)/1024)
	}
	fmt.Printf("m:%d\n", len(cbufs[len(cbufs)-1])/1024)
	fmt.Printf("containers total MB: %0.2fMB\n", float64(clen)/1024/1024)

	write(fout, fidx, header, cbufs)
}

func dumpEntities(db *sql.DB, fout, fidx *os.File) {
	// CREATE TABLE co_entity (id INTEGER PRIMARY KEY ASC, time INTEGER, data BLOB);
	rows, err := db.Query("select * from co_entity")
	if err != nil {
		log.Fatal(err)
	}
	var id, time int
	var meta []byte
	var bufs [2][]int
	var metas [][]byte
	for rows.Next() {
		rows.Scan(&id, &time, &meta)
		bufs[0] = append(bufs[0], id)
		bufs[1] = append(bufs[1], time)
		metas = append(metas, meta)
		if len(bufs[0])%100000 == 0 {
			fmt.Println("entities", len(bufs[0]))
		}
	}
	fmt.Println("entities", len(bufs[0]))

	cbufs, clen, header := compressBufs(bufs[:], metas)

	fmt.Printf("%d %dKB  ", len(bufs[0]), clen/1024)
	for i, cbuf := range cbufs[:2] {
		fmt.Printf("%s%d:%d ", "it"[i:i+1], cbuf[0], len(cbuf)/1024)
	}
	fmt.Printf("m:%d\n", len(cbufs[2])/1024)
	fmt.Printf("entities MB: %0.2fMB\n", float64(clen)/1024/1024)

	write(fout, fidx, header, cbufs)
}

func dumpSessions(db *sql.DB, fout, fidx *os.File) {
	// CREATE TABLE co_session (time INTEGER, user INTEGER, wid INTEGER, x INTEGER, y INTEGER, z INTEGER, action INTEGER);
	rows, err := db.Query("select * from co_session")
	if err != nil {
		log.Fatal(err)
	}
	var time, user, wid, x, y, z, action int
	var bufs [7][]int
	for rows.Next() {
		rows.Scan(&time, &user, &wid, &x, &y, &z, &action)
		bufs[0] = append(bufs[0], time)
		bufs[1] = append(bufs[1], user)
		bufs[2] = append(bufs[2], wid)
		bufs[3] = append(bufs[3], x)
		bufs[4] = append(bufs[4], y)
		bufs[5] = append(bufs[5], z)
		bufs[6] = append(bufs[6], action)
	}

	cbufs, clen, header := compressBufs(bufs[:], nil)

	fmt.Printf("%d %dKB  ", len(bufs[0]), clen/1024)
	for i, cbuf := range cbufs {
		fmt.Printf("%s%d:%d ", "tuwxyza"[i:i+1], cbuf[0], len(cbuf)/1024)
	}
	fmt.Printf("sessions MB: %0.2fMB\n", float64(clen)/1024/1024)

	write(fout, fidx, header, cbufs)
}

func dumpSigns(db *sql.DB, fout, fidx *os.File) {
	// CREATE TABLE co_sign (time INTEGER, user INTEGER, wid INTEGER, x INTEGER, y INTEGER, z INTEGER, line_1 TEXT, line_2 TEXT, line_3 TEXT, line_4 TEXT);
	rows, err := db.Query("select * from co_sign")
	if err != nil {
		log.Fatal(err)
	}
	var time, user, wid, x, y, z int
	var l1, l2, l3, l4 string
	var bufs [6][]int
	var metas [][]byte
	for rows.Next() {
		rows.Scan(&time, &user, &wid, &x, &y, &z, &l1, &l2, &l3, &l4)
		bufs[0] = append(bufs[0], time)
		bufs[1] = append(bufs[1], user)
		bufs[2] = append(bufs[2], wid)
		bufs[3] = append(bufs[3], x)
		bufs[4] = append(bufs[4], y)
		bufs[5] = append(bufs[5], z)
		l := strings.TrimRight(l1+"\n"+l2+"\n"+l3+"\n"+l4, "\n")
		// fmt.Println(time, user, wid, x, y, z, l)
		if len(l) > 0 {
			metas = append(metas, []byte(l))
		} else {
			metas = append(metas, nil)
		}
	}
	fmt.Println("signs", len(bufs[0]))

	cbufs, clen, header := compressBufs(bufs[:], metas)

	fmt.Printf("%d %dKB  ", len(bufs[0]), clen/1024)
	for i, cbuf := range cbufs[:6] {
		fmt.Printf("%s%d:%d ", "tuwxyz"[i:i+1], cbuf[0], len(cbuf)/1024)
	}
	fmt.Printf("m:%d\n", len(cbufs[6])/1024)
	fmt.Printf("signs MB: %0.2fMB\n", float64(clen)/1024/1024)

	write(fout, fidx, header, cbufs)
}

func dumpSkulls(db *sql.DB, fout, fidx *os.File) {
	// CREATE TABLE co_skull (id INTEGER PRIMARY KEY ASC, time INTEGER, type INTEGER, data INTEGER, rotation INTEGER, owner TEXT);
	rows, err := db.Query("select * from co_skull")
	if err != nil {
		log.Fatal(err)
	}
	var id, time, ty, data, rotation int
	var bufs [5][]int
	var owner []byte
	var metas [][]byte
	for rows.Next() {
		rows.Scan(&id, &time, &ty, &data, &rotation, &owner)
		bufs[0] = append(bufs[0], id)
		bufs[1] = append(bufs[1], time)
		bufs[2] = append(bufs[2], ty)
		bufs[3] = append(bufs[3], data)
		bufs[4] = append(bufs[4], rotation)
		if len(owner) > 0 {
			metas = append(metas, owner)
		} else {
			metas = append(metas, nil)
		}
	}
	fmt.Println("skulls", len(bufs[0]))

	cbufs, clen, header := compressBufs(bufs[:], metas)

	fmt.Printf("%d %dKB  ", len(bufs[0]), clen/1024)
	for i, cbuf := range cbufs[:5] {
		fmt.Printf("%s%d:%d ", "itydr"[i:i+1], cbuf[0], len(cbuf)/1024)
	}
	fmt.Printf("o:%d\n", len(cbufs[5])/1024)
	fmt.Printf("skulls MB: %0.2fMB\n", float64(clen)/1024/1024)

	write(fout, fidx, header, cbufs)
}

func dumpUsers(db *sql.DB, fout, fidx *os.File) {
	// CREATE TABLE co_user (id INTEGER PRIMARY KEY ASC, time INTEGER, user TEXT, uuid TEXT);
	// I don't think we care about the username_log??
	// CREATE TABLE co_username_log (id INTEGER PRIMARY KEY ASC, time INTEGER, uuid TEXT, user TEXT);
	rows, err := db.Query("select id, time, user from co_user")
	if err != nil {
		log.Fatal(err)
	}
	var id, time int
	var bufs [2][]int
	var user []byte
	var metas [][]byte
	for rows.Next() {
		rows.Scan(&id, &time, &user)
		bufs[0] = append(bufs[0], id)
		bufs[1] = append(bufs[1], time)
		metas = append(metas, user)
	}
	fmt.Println("users", len(bufs[0]))

	cbufs, clen, header := compressBufs(bufs[:], metas)

	fmt.Printf("%d %dKB  ", len(bufs[0]), clen/1024)
	for i, cbuf := range cbufs[:2] {
		fmt.Printf("%s%d:%d ", "it"[i:i+1], cbuf[0], len(cbuf)/1024)
	}
	fmt.Printf("m:%d\n", len(cbufs[2])/1024)
	fmt.Printf("users MB: %0.2fMB\n", float64(clen)/1024/1024)

	write(fout, fidx, header, cbufs)
}

func dumpMisc(db *sql.DB, fout, fidx *os.File) {
	/*
		CREATE TABLE co_art_map (id INTEGER, art TEXT);
		CREATE TABLE co_entity_map (id INTEGER, entity TEXT);
		CREATE TABLE co_material_map (id INTEGER, material TEXT);
		CREATE TABLE co_world (id INTEGER, world TEXT);

		skip: CREATE TABLE co_version (time INTEGER, version TEXT);
	*/
	misc := struct {
		Art      []string `json:"art"`
		Entity   []string `json:"entity"`
		Material []string `json:"material"`
		World    []string `json:"world"`
	}{[]string{""}, []string{""}, []string{""}, []string{""}}

	slurp := func(dest *[]string, query string) {
		rows, err := db.Query(query)
		if err != nil {
			log.Fatal(err)
		}
		var x string
		for rows.Next() {
			rows.Scan(&x)
			*dest = append(*dest, x)
		}
	}
	slurp(&misc.Art, "select art from co_art_map")
	slurp(&misc.Entity, "select entity from co_entity_map")
	slurp(&misc.Material, "select material from co_material_map")
	slurp(&misc.World, "select world from co_world")
	b, err := json.Marshal(misc)
	if err != nil {
		log.Fatal(err)
	}

	var header [5]byte
	hlen := binary.PutVarint(header[:], int64(len(b)))

	fmt.Printf("misc %dKB\n", len(b)/1024)
	write(fout, fidx, header[:hlen], [][]byte{b})
}

func openFiles(name string) (a, b *os.File) {
	fout, err := os.Create("co_" + name + ".db")
	if err != nil {
		log.Fatal(err)
	}
	fidx, err := os.Create("co_" + name + ".idx")
	if err != nil {
		log.Fatal(err)
	}
	return fout, fidx
}

func write(fout, fidx *os.File, header []byte, cbufs [][]byte) {
	_, err := fidx.Write(header)
	if err != nil {
		log.Fatal(err)
	}
	_, err = fout.Write(header)
	if err != nil {
		log.Fatal(err)
	}
	for _, cbuf := range cbufs {
		_, err = fout.Write(cbuf)
		if err != nil {
			log.Fatal(err)
		}
	}
}

func main() {
	db, err := sql.Open("sqlite3", "./database.db")
	if err != nil {
		log.Fatal(err)
	}

	fout, fidx := openFiles("out")
	defer fout.Close()
	defer fidx.Close()
	dumpMisc(db, fout, fidx)
	dumpUsers(db, fout, fidx)
	dumpSkulls(db, fout, fidx)
	dumpSigns(db, fout, fidx)
	dumpSessions(db, fout, fidx)
	dumpContainers(db, fout, fidx)
	dumpEntities(db, fout, fidx)

	blockOut, blockIdx := openFiles("blocks")
	defer blockOut.Close()
	defer blockIdx.Close()
	dumpBlocks(db, blockOut, blockIdx)
}

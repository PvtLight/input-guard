// Command mkres writes app/rsrc_windows_amd64.syso: a COFF object holding the
// .exe's Windows resources (the manifest, the icon, and the version details
// Task Manager and Explorer show). Go links any .syso in the package folder.
// It needs nothing outside the Go standard library.
//
//	go run ./tools/mkres -version 0.1.0 -manifest app/InputGuard.manifest -o app/rsrc_windows_amd64.syso
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"inputguard/art"
)

const (
	rtIcon      = 3
	rtGroupIcon = 14
	rtVersion   = 16
	rtManifest  = 24
	langEnUS    = 0x0409
)

type resource struct {
	typ, id uint16
	data    []byte
}

func main() {
	version := flag.String("version", "0.1.0", "version, as major.minor.patch")
	manifestPath := flag.String("manifest", "app/InputGuard.manifest", "application manifest")
	out := flag.String("o", "app/rsrc_windows_amd64.syso", "output object")
	flag.Parse()

	manifest, err := os.ReadFile(*manifestPath)
	if err != nil {
		log.Fatal(err)
	}
	var resources []resource
	group := new(bytes.Buffer)
	sizes := []int{16, 20, 24, 32, 40, 48, 64, 256}
	binary.Write(group, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	for i, size := range sizes {
		data := iconImage(art.BrandMark(size))
		id := uint16(i + 1)
		resources = append(resources, resource{rtIcon, id, data})
		dim := uint8(size)
		if size >= 256 {
			dim = 0 // 0 means 256
		}
		group.Write([]byte{dim, dim, 0, 0})
		binary.Write(group, binary.LittleEndian, uint16(1))  // planes
		binary.Write(group, binary.LittleEndian, uint16(32)) // bits per pixel
		binary.Write(group, binary.LittleEndian, uint32(len(data)))
		binary.Write(group, binary.LittleEndian, id)
	}
	resources = append(resources,
		resource{rtGroupIcon, 1, group.Bytes()},
		resource{rtVersion, 1, versionInfo(*version)},
		resource{rtManifest, 1, manifest},
	)
	if err := os.WriteFile(*out, coff(resources), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote %s: %d resources, manifest %d bytes, icon in %d sizes\n", *out, len(resources), len(manifest), len(sizes))
}

// iconImage is one size of the icon: PNG for 256 pixels, a 32-bit DIB with
// its transparency mask for the rest, as Windows expects.
func iconImage(img *image.NRGBA) []byte {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if w >= 256 {
		var b bytes.Buffer
		if err := png.Encode(&b, img); err != nil {
			log.Fatal(err)
		}
		return b.Bytes()
	}
	maskRow := (w + 31) / 32 * 4
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, struct {
		Size                      uint32
		Width, Height             int32
		Planes, BitCount          uint16
		Compression, SizeImage    uint32
		XPels, YPels              int32
		ColorsUsed, ColorsImportn uint32
	}{40, int32(w), int32(2 * h), 1, 32, 0, uint32(w*h*4 + maskRow*h), 0, 0, 0, 0})
	for y := h - 1; y >= 0; y-- { // bottom-up
		for x := 0; x < w; x++ {
			c := img.NRGBAAt(x, y)
			b.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	for y := h - 1; y >= 0; y-- {
		row := make([]byte, maskRow)
		for x := 0; x < w; x++ {
			if img.NRGBAAt(x, y).A == 0 {
				row[x/8] |= 0x80 >> (x % 8)
			}
		}
		b.Write(row)
	}
	return b.Bytes()
}

func utf16z(s string) []byte {
	var b bytes.Buffer
	for _, u := range utf16.Encode([]rune(s)) {
		binary.Write(&b, binary.LittleEndian, u)
	}
	b.Write([]byte{0, 0})
	return b.Bytes()
}

func pad4(b *bytes.Buffer) {
	for b.Len()%4 != 0 {
		b.WriteByte(0)
	}
}

// block is one node of a VS_VERSIONINFO tree.
func block(key string, text bool, value []byte, valueLength uint16, children ...[]byte) []byte {
	var b bytes.Buffer
	kind := uint16(0)
	if text {
		kind = 1
	}
	binary.Write(&b, binary.LittleEndian, [3]uint16{0, valueLength, kind})
	b.Write(utf16z(key))
	pad4(&b)
	b.Write(value)
	for _, child := range children {
		pad4(&b)
		b.Write(child)
	}
	out := b.Bytes()
	binary.LittleEndian.PutUint16(out, uint16(len(out)))
	return out
}

func versionInfo(version string) []byte {
	var parts [4]uint16
	for i, p := range strings.SplitN(version, ".", 4) {
		n, err := strconv.Atoi(p)
		if err != nil {
			log.Fatalf("bad version %q", version)
		}
		parts[i] = uint16(n)
	}
	ms := uint32(parts[0])<<16 | uint32(parts[1])
	ls := uint32(parts[2])<<16 | uint32(parts[3])
	fixed := new(bytes.Buffer)
	binary.Write(fixed, binary.LittleEndian, [13]uint32{
		0xFEEF04BD, 0x00010000, ms, ls, ms, ls,
		0x3F, 0, 0x00040004, 1, 0, 0, 0, // flags mask, flags, VOS_NT_WINDOWS32, VFT_APP
	})
	strs := [][2]string{
		{"CompanyName", "Input Guard"},
		{"FileDescription", "Input Guard"},
		{"FileVersion", version},
		{"InternalName", "InputGuard"},
		{"LegalCopyright", "MIT License"},
		{"OriginalFilename", "InputGuard.exe"},
		{"ProductName", "Input Guard"},
		{"ProductVersion", version},
	}
	var entries [][]byte
	for _, kv := range strs {
		value := utf16z(kv[1])
		entries = append(entries, block(kv[0], true, value, uint16(len(value)/2)))
	}
	table := block("040904B0", true, nil, 0, entries...)
	stringInfo := block("StringFileInfo", true, nil, 0, table)
	translation := block("Translation", false, []byte{0x09, 0x04, 0xB0, 0x04}, 4)
	varInfo := block("VarFileInfo", true, nil, 0, translation)
	return block("VS_VERSION_INFO", false, fixed.Bytes(), uint16(fixed.Len()), stringInfo, varInfo)
}

// coff lays out the resource tree (type, then ID, then language) and its data
// in one .rsrc section. Each data entry's address is relocated against the
// section, which the Go linker resolves when it builds the .exe.
func coff(resources []resource) []byte {
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].typ != resources[j].typ {
			return resources[i].typ < resources[j].typ
		}
		return resources[i].id < resources[j].id
	})
	var types []uint16
	byType := map[uint16][]int{}
	for i, r := range resources {
		if len(byType[r.typ]) == 0 {
			types = append(types, r.typ)
		}
		byType[r.typ] = append(byType[r.typ], i)
	}
	dirSize := func(entries int) int { return 16 + 8*entries }
	// Offsets of everything in the section.
	offset := dirSize(len(types))
	typeDir := map[uint16]int{}
	for _, t := range types {
		typeDir[t] = offset
		offset += dirSize(len(byType[t]))
	}
	langDir := make([]int, len(resources))
	for i := range resources {
		langDir[i] = offset
		offset += dirSize(1)
	}
	dataEntry := make([]int, len(resources))
	for i := range resources {
		dataEntry[i] = offset
		offset += 16
	}
	dataAt := make([]int, len(resources))
	for i, r := range resources {
		offset = (offset + 7) &^ 7
		dataAt[i] = offset
		offset += len(r.data)
	}
	offset = (offset + 3) &^ 3
	section := make([]byte, offset)
	le := binary.LittleEndian
	dir := func(at, ids int) { le.PutUint16(section[at+14:], uint16(ids)) } // ID entries only
	entry := func(at, n int, id uint32, target int) {
		le.PutUint32(section[at+16+8*n:], id)
		le.PutUint32(section[at+16+8*n+4:], uint32(target))
	}
	dir(0, len(types))
	for n, t := range types {
		entry(0, n, uint32(t), typeDir[t]|1<<31)
		dir(typeDir[t], len(byType[t]))
		for k, i := range byType[t] {
			entry(typeDir[t], k, uint32(resources[i].id), langDir[i]|1<<31)
		}
	}
	for i, r := range resources {
		dir(langDir[i], 1)
		entry(langDir[i], 0, langEnUS, dataEntry[i])
		le.PutUint32(section[dataEntry[i]:], uint32(dataAt[i])) // relocated to an RVA
		le.PutUint32(section[dataEntry[i]+4:], uint32(len(r.data)))
		copy(section[dataAt[i]:], r.data)
	}

	var obj bytes.Buffer
	const headers = 20 + 40
	relocAt := headers + len(section)
	symbolsAt := relocAt + 10*len(resources)
	binary.Write(&obj, le, struct {
		Machine, Sections               uint16
		Time, Symbols, SymbolCount      uint32
		OptionalHeader, Characteristics uint16
	}{0x8664, 1, 0, uint32(symbolsAt), 1, 0, 0})
	binary.Write(&obj, le, struct {
		Name                                      [8]byte
		VirtualSize, VirtualAddress, RawSize, Raw uint32
		Relocations, LineNumbers                  uint32
		RelocationCount, LineNumberCount          uint16
		Characteristics                           uint32
	}{[8]byte{'.', 'r', 's', 'r', 'c'}, 0, 0, uint32(len(section)), headers,
		uint32(relocAt), 0, uint16(len(resources)), 0, 0x40000040}) // initialized data, read
	obj.Write(section)
	for i := range resources {
		binary.Write(&obj, le, uint32(dataEntry[i])) // where the address goes
		binary.Write(&obj, le, uint32(0))            // the section's symbol
		binary.Write(&obj, le, uint16(3))            // IMAGE_REL_AMD64_ADDR32NB
	}
	obj.Write([]byte{'.', 'r', 's', 'r', 'c', 0, 0, 0}) // symbol 0: the section
	binary.Write(&obj, le, uint32(0))                   // value
	binary.Write(&obj, le, int16(1))                    // section number
	binary.Write(&obj, le, uint16(0))                   // type
	obj.Write([]byte{3, 0})                             // static, no aux symbols
	binary.Write(&obj, le, uint32(4))                   // empty string table
	return obj.Bytes()
}

// Command checkexe opens the built InputGuard.exe the way Windows would read
// it, without Windows: a 64-bit GUI program whose resource tree holds the
// manifest (well-formed, run as administrator when the account allows), the icon in
// every size, and the version details.
//
//	go run ./tools/checkexe InputGuard.exe app/InputGuard.manifest
package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"os"
	"strings"
	"unicode/utf16"
)

var failed bool

func check(ok bool, format string, args ...any) {
	mark := "PASS"
	if !ok {
		mark, failed = "FAIL", true
	}
	fmt.Printf("%s: %s\n", mark, fmt.Sprintf(format, args...))
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: checkexe InputGuard.exe InputGuard.manifest")
		os.Exit(2)
	}
	wantManifest, err := os.ReadFile(os.Args[2])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	f, err := pe.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer f.Close()
	opt, ok := f.OptionalHeader.(*pe.OptionalHeader64)
	check(ok && f.Machine == pe.IMAGE_FILE_MACHINE_AMD64, "64-bit x64 program")
	if !ok {
		os.Exit(1)
	}
	check(opt.Subsystem == pe.IMAGE_SUBSYSTEM_WINDOWS_GUI, "GUI program: no console window opens")
	const aslr, nx = 0x0040, 0x0100
	check(opt.DllCharacteristics&aslr != 0 && opt.DllCharacteristics&nx != 0, "address randomization and no-execute data are on (0x%04x)", opt.DllCharacteristics)

	dirEntry := opt.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_RESOURCE]
	check(dirEntry.VirtualAddress != 0 && dirEntry.Size > 0, "resource directory present (%d bytes)", dirEntry.Size)
	var rsrc *pe.Section
	for _, s := range f.Sections {
		if dirEntry.VirtualAddress >= s.VirtualAddress && dirEntry.VirtualAddress < s.VirtualAddress+s.VirtualSize {
			rsrc = s
		}
	}
	if rsrc == nil {
		check(false, "resource directory is inside a section")
		os.Exit(1)
	}
	data, err := rsrc.Data()
	check(err == nil, "resource section %q readable", rsrc.Name)
	base := dirEntry.VirtualAddress - rsrc.VirtualAddress
	tree := data[base:]
	le := binary.LittleEndian
	// read returns the resource data for type/id/any language.
	read := func(typ, id uint32) ([]byte, bool) {
		find := func(dir uint32, want uint32, any bool) (uint32, bool) {
			named, ids := le.Uint16(tree[dir+12:]), le.Uint16(tree[dir+14:])
			for i := uint32(0); i < uint32(named)+uint32(ids); i++ {
				at := dir + 16 + 8*i
				if any || le.Uint32(tree[at:]) == want {
					return le.Uint32(tree[at+4:]), true
				}
			}
			return 0, false
		}
		t, ok := find(0, typ, false)
		if !ok || t&(1<<31) == 0 {
			return nil, false
		}
		n, ok := find(t&^(1<<31), id, false)
		if !ok || n&(1<<31) == 0 {
			return nil, false
		}
		l, ok := find(n&^(1<<31), 0, true)
		if !ok || l&(1<<31) != 0 {
			return nil, false
		}
		rva, size := le.Uint32(tree[l:]), le.Uint32(tree[l+4:])
		if rva < rsrc.VirtualAddress || rva+size > rsrc.VirtualAddress+uint32(len(data)) {
			return nil, false
		}
		return data[rva-rsrc.VirtualAddress : rva-rsrc.VirtualAddress+size], true
	}

	manifest, ok := read(24, 1)
	check(ok && bytes.Equal(manifest, wantManifest), "manifest (resource 24/1) is the file's %d bytes, at its relocated address", len(wantManifest))
	var doc struct {
		XMLName    xml.Name
		Dependency []struct {
			Assembly struct {
				Identity struct {
					Name    string `xml:"name,attr"`
					Version string `xml:"version,attr"`
				} `xml:"assemblyIdentity"`
			} `xml:"dependentAssembly"`
		} `xml:"dependency"`
		Trust struct {
			Level struct {
				Level    string `xml:"level,attr"`
				UIAccess string `xml:"uiAccess,attr"`
			} `xml:"security>requestedPrivileges>requestedExecutionLevel"`
		} `xml:"trustInfo"`
		DPI string `xml:"application>windowsSettings>dpiAwareness"`
	}
	err = xml.Unmarshal(manifest, &doc)
	check(err == nil && doc.XMLName.Local == "assembly", "manifest is well-formed XML (%v)", err)
	check(doc.Trust.Level.Level == "highestAvailable" && doc.Trust.Level.UIAccess == "false",
		"manifest runs it as administrator for admin accounts (one prompt at launch), so its hooks also cover admin windows")
	check(len(doc.Dependency) == 1 && doc.Dependency[0].Assembly.Identity.Name == "Microsoft.Windows.Common-Controls" &&
		doc.Dependency[0].Assembly.Identity.Version == "6.0.0.0", "manifest selects the modern Windows controls")
	check(strings.HasPrefix(doc.DPI, "PerMonitorV2"), "manifest makes the app sharp on every display (%s)", doc.DPI)

	group, ok := read(14, 1)
	check(ok && len(group) >= 6 && le.Uint16(group[2:]) == 1, "icon group (resource 14/1)")
	if ok {
		count := int(le.Uint16(group[4:]))
		var sizes []string
		good := len(group) == 6+14*count
		for i := 0; good && i < count; i++ {
			e := group[6+14*i:]
			dim, bytesInRes, id := int(e[0]), le.Uint32(e[8:]), le.Uint16(e[12:])
			if dim == 0 {
				dim = 256
			}
			img, found := read(3, uint32(id))
			isPNG := bytes.HasPrefix(img, []byte("\x89PNG\r\n\x1a\n"))
			isDIB := len(img) >= 40 && le.Uint32(img) == 40 && int(le.Uint32(img[4:])) == dim && int(le.Uint32(img[8:])) == 2*dim
			good = found && uint32(len(img)) == bytesInRes && (isPNG || isDIB)
			sizes = append(sizes, fmt.Sprint(dim))
		}
		check(good, "icon images present in sizes %s", strings.Join(sizes, ", "))
	}

	version, ok := read(16, 1)
	description := ""
	if ok {
		// The strings are UTF-16; find "FileDescription" and the value after it.
		u := make([]uint16, len(version)/2)
		for i := range u {
			u[i] = le.Uint16(version[2*i:])
		}
		text := string(utf16.Decode(u))
		if i := strings.Index(text, "FileDescription\x00"); i >= 0 {
			rest := strings.TrimLeft(text[i+len("FileDescription\x00"):], "\x00")
			description, _, _ = strings.Cut(rest, "\x00")
		}
	}
	check(ok && le.Uint16(version) == uint16(len(version)) && bytes.Contains(version, []byte{0xBD, 0x04, 0xEF, 0xFE}) && description == "Input Guard",
		"version details (resource 16/1): Task Manager shows %q", description)
	if failed {
		os.Exit(1)
	}
}

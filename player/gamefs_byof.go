//go:build byof

package main

// Bring-your-own-files build: no game data is compiled in. The player finds the
// player's own copy of the DOS game at startup and checks every file against the
// hashes of the release it was built for, so the patches always land on the code
// they were written for.
//
// Where it looks, in order:
//  1. a folder given on the command line (or dropped onto Rampart.exe)
//  2. the RAMPART_GAME environment variable
//  3. the folder remembered from the last successful start (gamedir.txt in the save folder)
//  4. the folder holding Rampart.exe, then its subfolders, two levels deep
//     (e.g. Rampart.exe next to "RAMPART" or "Rampart_DOS_EN\RAMPART")

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing/fstest"
)

const byofBuild = true

// SHA-256 of each game file of the HAL9000 / I.N.C English release.
var wantFiles = map[string]string{
	"M1PPF.RSC":   "1ee5e1c717e4456f349bc15a45d8697bdcfe9e4c36503bae0642810fe2f77662",
	"M2PPF.RSC":   "d39b6adc3410f3c3349a94652d1946e0652d719804f56b09ebc309cbccd97a7f",
	"M3PPF.RSC":   "c29ec8f26c53f9592fa3a146f9375277e4830d77b3c2caa1dfe2f1e2ba571840",
	"MBATPF.GRA":  "59ff251a73a8ee1399b020f41fb1c8a40a99268639014299b92ca515abb2fb9b",
	"MHSCPF.GRA":  "6d63a4ce26426a66788476c49880bdb7e7d2d1fb744ba08f4f2af5e1999aec68",
	"MPAUSE.GRA":  "17f6aa6f2cef9a730040abc22ebaeb294592864e1650fef840f2ca355bf0739f",
	"MTETPF.GRA":  "bdb7a3dffd6b5b951cb656fa2c79ed1ce52a30289698f5e5a684a3ef8fee0f41",
	"MTIT2PF.GRA": "e831c86b869bd3271be45bcae10946efd2110c395c13b65588c5f6e9020bc827",
	"MTITLE.RSC":  "2d09e2720bddbf2c6c382e8687ff6911c5e1842cafe20ac57c6f5e8b5918938d",
	"MTITPF.GRA":  "08196f522606722615ccb5ae99025b30dc47fc8ceab8785fa677fae51c0f20fc",
	"MTRANS.RSC":  "7f67f5f4972dd05bf4b100d21c44a8029f547ac48b1d274d59898ec023b341e3",
	"MTRNPF.GRA":  "042f4a794deae2872a120436e14257fa3bf7f3c04470f87bd55817abc7f0dd39",
	"MXPLPF.GRA":  "7a21d0a826bfc63ab7ab1c5497f2338c3acbef14773eb42b2c41b36f2927b6a8",
	"PAUSE.STR":   "aab927b4910da0ec95c109fd9c1325b9f4ec7c6d80ab3fe60f5bcd3d48c8f1b5",
	"RAMP.AD":     "b5e14a2577d19b8841b9fa176ed2f91b37bd81d2e11becf3d0295a6583712d4c",
	"RAMPART.EXE": "05e14a4ed47d02b95608b17c6355f53db1fdb44c6d27619443c76c7b68eb2d17",
	"RMUSIC.RSC":  "238b71c463575474afbee36d17abe6076f2b881f286f11946aa68b9bca636157",
	"SOUND.RSC":   "54575e113b73e84bde130ca7942ce595e85a9450cca1422aabf228674064587b",
	"XLOGO2":      "5f6af4edc5e9ba32d56b68b4a77dc096ebacdaf359b0463066d85f9350aafc47",
}

// Default game settings (plaintext RAMPART.CFG words: no sound card, the
// default keys, default options), used when the copy has no RAMPART.CFG.
const defaultCfgHex = "0100ffffffffffff040028000e000000000000000000040028000e000000000000000000050000000000000000000600" +
	"0a00480050004c004b004d001c001b00000000000000000006000a0020002e002e002d002f002a001d000000000000000000"

func rememberFile() string {
	if d, err := os.UserConfigDir(); err == nil {
		return filepath.Join(d, "Rampart", "gamedir.txt")
	}
	return ""
}

// listUpper maps upper-case file names in dir to their real names.
func listUpper(dir string) map[string]string {
	out := map[string]string{}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range ents {
		if !e.IsDir() {
			out[strings.ToUpper(e.Name())] = e.Name()
		}
	}
	return out
}

func hasGame(dir string) bool {
	_, ok := listUpper(dir)["RAMPART.EXE"]
	return ok
}

func candidates() []string {
	var c []string
	for _, a := range os.Args[1:] {
		if st, err := os.Stat(a); err == nil {
			if st.IsDir() {
				c = append(c, a)
			} else {
				c = append(c, filepath.Dir(a)) // RAMPART.EXE itself dropped on us
			}
		}
	}
	if d := os.Getenv("RAMPART_GAME"); d != "" {
		c = append(c, d)
	}
	if p := rememberFile(); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			c = append(c, strings.TrimSpace(string(b)))
		}
	}
	exe, _ := os.Executable()
	base := filepath.Dir(exe)
	c = append(c, base)
	var sub func(dir string, depth int)
	sub = func(dir string, depth int) {
		ents, _ := os.ReadDir(dir)
		for _, e := range ents {
			if e.IsDir() {
				p := filepath.Join(dir, e.Name())
				c = append(c, p)
				if depth > 1 {
					sub(p, depth-1)
				}
			}
		}
	}
	sub(base, 2)
	return c
}

func loadGameFS() (fs.FS, string, error) {
	dir := ""
	for _, d := range candidates() {
		if d != "" && hasGame(d) {
			dir = d
			break
		}
	}
	empty := fstest.MapFS{}
	if dir == "" {
		return empty, "", fmt.Errorf("Rampart: Refortified needs your own copy of DOS Rampart " +
			"(the English release unprotected by HAL9000 / I.N.C).\n\n" +
			"Put the game's folder (the one with RAMPART.EXE in it) next to Rampart.exe, " +
			"or drag that folder onto Rampart.exe.\n\nSee README.txt for details.")
	}
	files := listUpper(dir)
	mfs := fstest.MapFS{}
	var missing, wrong []string
	names := make([]string, 0, len(wantFiles))
	for n := range wantFiles {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		real, ok := files[n]
		if !ok {
			missing = append(missing, n)
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, real))
		if err != nil {
			missing = append(missing, n)
			continue
		}
		s := sha256.Sum256(b)
		if hex.EncodeToString(s[:]) != wantFiles[n] {
			wrong = append(wrong, n)
		}
		mfs["game/"+n] = &fstest.MapFile{Data: b, Mode: 0o444}
	}
	if len(missing) > 0 || len(wrong) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "The game files in\n%s\ndon't match the release this mod was made for "+
			"(English DOS Rampart, unprotected by HAL9000 / I.N.C).\n", dir)
		if len(missing) > 0 {
			fmt.Fprintf(&b, "\nMissing: %s\n", strings.Join(missing, ", "))
		}
		if len(wrong) > 0 {
			fmt.Fprintf(&b, "\nDifferent from the expected copy: %s\n", strings.Join(wrong, ", "))
		}
		b.WriteString("\nREADME.txt lists the exact file hashes.")
		return empty, dir, fmt.Errorf("%s", b.String())
	}
	// RAMPART.CFG and RAMPART.HIS change as the game is played; take the copy's
	// own when it has them.
	if real, ok := files["RAMPART.CFG"]; ok {
		if b, err := os.ReadFile(filepath.Join(dir, real)); err == nil && len(b) >= 0x66 {
			mfs["game/RAMPART.CFG"] = &fstest.MapFile{Data: b, Mode: 0o444}
		}
	}
	if _, ok := mfs["game/RAMPART.CFG"]; !ok {
		p, _ := hex.DecodeString(defaultCfgHex)
		sum := cfgChecksum(p)
		enc := append([]byte(nil), p...)
		cfgCrypt(enc)
		enc = binary.LittleEndian.AppendUint32(enc, sum)
		mfs["game/RAMPART.CFG"] = &fstest.MapFile{Data: enc, Mode: 0o444}
	}
	if real, ok := files["RAMPART.HIS"]; ok {
		if b, err := os.ReadFile(filepath.Join(dir, real)); err == nil {
			mfs["game/RAMPART.HIS"] = &fstest.MapFile{Data: b, Mode: 0o444}
		}
	}
	if p := rememberFile(); p != "" {
		if abs, err := filepath.Abs(dir); err == nil {
			os.MkdirAll(filepath.Dir(p), 0o755)
			os.WriteFile(p, []byte(abs), 0o644)
		}
	}
	return mfs, dir, nil
}

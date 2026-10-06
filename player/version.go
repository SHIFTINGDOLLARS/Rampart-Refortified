package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Version of this player. Bump it for every release and add a line to NOTES.md.
//
//	7.x  sound, controls, colours, rules, Endless, Yappy Captain
//	8.0  computer players (join screen 1/2/3), Smart Pieces
//	8.1  Endless records, About page
//	8.2  bots: fewer shots at tough cannons, keep expanding, real enclosure check, take MP continues
//	8.3  bots pack cannons tightly; balloons (Normal+) and super cannons (Hard)
//	8.4  Xbox (XInput) gamepads; Controls page in F12
//	8.5  grunt busters (special cannon: five-ball + that only kills grunts)
//	8.6  grunt busters tinted black in the battle view
//	8.7  battle-view buster: plain cannon in neutral black, floor left alone
//	8.8  battle-view buster silver, only the buster; one MP "alive" line per build phase
//	8.9  battle-view buster stays silver after a continue changes the cannon look
//	9.0  release package: the player reads your own copy of the game (byof build)
const Version = "9.0"

const (
	ModName   = "Rampart: Refortified"
	ModAuthor = "Shifting Dollars"
	ModURL    = "github.com/SHIFTINGDOLLARS"
)

// GameHashes identify the game files this player was built against. The patches
// sit at fixed addresses in RAMPART.EXE, so only this exact build works.
//
//	Exe: SHA-256 of RAMPART.EXE.
//	Set: SHA-256 over every other game file (name, NUL, contents, in name order),
//	     leaving out RAMPART.CFG and RAMPART.HIS, which the player rewrites.
type GameHashes struct {
	Exe, Set string
	Files    int
}

func computeGameHashes(fsys fs.FS) GameHashes {
	var h GameHashes
	ents, err := fs.ReadDir(fsys, "game")
	if err != nil {
		return h
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() && e.Name() != "RAMPART.CFG" && e.Name() != "RAMPART.HIS" {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	set := sha256.New()
	for _, n := range names {
		b, err := fs.ReadFile(fsys, "game/"+n)
		if err != nil {
			continue
		}
		set.Write([]byte(n))
		set.Write([]byte{0})
		set.Write(b)
		if n == "RAMPART.EXE" {
			s := sha256.Sum256(b)
			h.Exe = hex.EncodeToString(s[:])
		}
	}
	h.Set = hex.EncodeToString(set.Sum(nil))
	h.Files = len(names)
	return h
}

// The release this player is built for. Its credits scroll thanks "HAL9000 from
// I.N.C for *UNPROTECTING* this fine Bitmasters game" and the exe carries the
// string "Crk by HAL9000.", which is how this release is told apart from other
// DOS copies of Rampart.
const (
	knownExeSHA256 = "05e14a4ed47d02b95608b17c6355f53db1fdb44c6d27619443c76c7b68eb2d17"
	releaseName    = "HAL9000 / I.N.C"
)

func (h GameHashes) release() string {
	if h.Exe == knownExeSHA256 {
		return releaseName
	}
	return "UNKNOWN"
}

var gameHashes GameHashes

func init() { gameHashes = computeGameHashes(gameFiles) }

// aboutText is what a player copies when reporting a problem or asking what to
// download: the player version and the game files it expects.
func aboutText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s, a mod by %s\n", ModName, Version, ModAuthor)
	fmt.Fprintf(&b, "Official source: https://%s\n", ModURL)
	fmt.Fprintf(&b, "Unofficial fan mod. Rampart (c) 1990 Atari Games; DOS version by Bitmasters.\n\n")
	fmt.Fprintf(&b, "Game release: English DOS, unprotected by %s (credits scroll), %s\n", releaseName,
		map[bool]string{true: "this copy matches", false: "THIS COPY DOES NOT MATCH"}[gameHashes.Exe == knownExeSHA256])
	if gameDir != "" {
		fmt.Fprintf(&b, "Game folder: %s\n", gameDir)
	}
	fmt.Fprintf(&b, "Game files: %d (RAMPART.CFG and RAMPART.HIS excluded)\n", gameHashes.Files)
	fmt.Fprintf(&b, "RAMPART.EXE SHA-256: %s\n", gameHashes.Exe)
	fmt.Fprintf(&b, "Game file set SHA-256: %s\n", gameHashes.Set)
	return b.String()
}

// writeAbout saves aboutText as about.txt in the save folder (best effort).
func writeAbout(dir string) {
	if dir != "" {
		os.WriteFile(filepath.Join(dir, "about.txt"), []byte(aboutText()), 0o644)
	}
}

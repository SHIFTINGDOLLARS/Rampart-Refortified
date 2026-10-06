# Rampart (DOS, 1992 Bitmasters/EA): reverse-engineering notes

Source: `Rampart_DOS_EN.zip` (the user's copy). It is a cracked release: the manual-lookup
copy protection (module `copypro`, "SELECT THE 3 IMAGES…") is bypassed, and the credits
have a "HAL9000 / I.N.C" line added.

Addresses are image offsets in the unpacked program. Code is all in segment 0 (`CS:IP` =
image offset). Data addresses are `DS:` offsets, where DS = image segment `0E4E`, which is
`165E` when loaded at PSP `0800` as the emulator does.

Confidence markers: **[verified]** = checked against the running game in the emulator;
**[code]** = read from disassembly, not yet exercised; **[guess]** = a hypothesis.
**Note from Shifting Dollars:** Claude code wrote this. It can't play the game. I will update this with any knowledge I have/discover, along with anything anyone else can confirm.

---------------------------------------------------------------------------------------------

## 1. Quick start

| Tool | What it does |
|---|---|
| `tools/unpack.py RAMPART.EXE out.bin` | strips the outer LZ packer |
| `tools/unexepack.py out.bin RAMPART_UNPACKED.EXE` | strips EXEPACK, writes a normal MZ EXE (+ `.bin` flat image) |
| `tools/rsc.py *.RSC` | lists archive contents |
| `tools/extract.py` | writes every image, map, tileset, banner, box, sound and song to `out/assets/` |
| `tools/pft.py` | PFT map tilemap decoder (used by extract) |
| `tools/enclosure.py` | exact port of the territory/enclosure algorithm, with self-tests |
| `tools/cfgfiles.py` | decodes `RAMPART.CFG` and `RAMPART.HIS` (both checksums verify) |
| `tools/annotate.py` + `tools/labels.txt` | annotated disassembly → `out/rampart_seg0.asm` |
| `emu/emu.c`, `emu/harness.py` | headless DOS/VGA emulator that runs the real game; scripted input, screenshots, memory dumps/pokes |

Build the emulator: `gcc -O2 -o emu/emu emu/emu.c`. Run it with
`emu/emu <gamedir> <exe> <script> <maxframes> [-v]`.

Script lines are `<frame> <cmd> …`, where cmd is one of:
`key <scancode-hex> [holdframes]`, `shot <file.ppm>`, `dac <file>`,
`dump <seg> <off> <len> <file>`, `poke <seg> <off> <hexbytes>`, `peek <seg> <off> <len>`,
`stack`, `prof 1|0`, `regs`, `quit`.

`harness.TO_GAME` is the key sequence that reaches a one-player game as Blue / Recruit.
It skips the intro, presses F1 twice, Enter to join, and Enter for Recruit. Enter at frames
2950 and 3200 then selects the home castle.

---------------------------------------------------------------------------------------------

## 2. The executable

**Two packing layers [verified]:**
1. Outer: a custom LZ packer, a relative of LZEXE. The stub relocates the image up by 0x8F6
   paragraphs and jumps to the decoder at image offset 0xC2BA. The bit stream is 16-bit
   little-endian control words, LSB first. Codes:
   - `1` → literal byte.
   - `00b`+ → short match of length 2: 8-bit distance, or 11-bit distance with an extra 3 bits.
   - `01` → long match: 13-bit distance; length 3–6 unary, then 7/8, 9–16 (3 bits), or
     byte+17.
   - Distance byte `FF` with high byte `FF` + bit 0 → end of stream. With bit 1, it is a
     segment-normalise marker.
2. Inner: Microsoft EXEPACK (`RB` signature at segment 0x1498). It has 92 relocations, and
   the true entry point is 0000:0000 with the stack at 8806:1400.

**Load image layout [code]:**

| Segment (image-relative) | Contents |
|---|---|
| `0000`–`0D82` | all game code (hand-written assembly, near calls) |
| `0D83` | Miles AIL sound library (far-called) |
| `0E4E` | DS = SS: all variables, tables, strings, grids |
| `1D80`, `2510`, `3510`, `42A0`…`82A0` | 64 KB work buffers (screens, tiles, sprites) |
| `8806` | stack |

**Command line:** a `c` argument opens the Configuration screen directly.

**Video [verified]:** VGA mode 13h. The game switches between chained (`SEQ4=0E`) and
unchained (`SEQ4=06`) memory, uses the CRTC start address for scrolling, and uses
line-compare for split-screen wipes. Frame pacing is a busy-wait on the `3DA` retrace bit,
so one game frame = 1/70 s.

**Input [verified]:**
- The keyboard IRQ handler at `04E0` keeps `DS:2151[scancode]`. A press sets `C0` and a
  release clears bit 7, which leaves bit 6 as a "latched press".
- Menus clear the latches (`051A`) before polling.
- Default keyboard 1 is arrows / Enter=fire / `]`=rotate. Keyboard 2 is D C X V,
  LShift=fire, Ctrl=rotate.
- Joystick and mouse are also supported.

**RNG [code]:** a 16-bit Galois LFSR at `DS:22CD`: `x = (x>>1) ^ (carry ? B400 : 0)`.
`random(n) = (x*n)>>16`. It is seeded from the DOS time (CX+DX).

---------------------------------------------------------------------------------------------

## 3. Data files

| File | Format |
|---|---|
| `*.RSC` | Archive. `u16 count`, 18 pad bytes, then `count` × {`char name[12]`, `u32 offset`, `u32 size`}. |
| `*.GRA` | Raw 8×8 tiles, 64 bytes each, 8-bit palette indices. Tile 0 is blank. |
| `M1PPF.RSC` / `M2PPF` / `M3PPF` | Maps for 1, 2 and 3 players: 7 / 6 / 6 maps. |
| `PALETTE` (768) | 6-bit VGA DAC. It equals the in-game DAC exactly **[verified]**. |
| `PFT_n` | Map tilemap actually drawn in build/cannon phases. 40×25 u16 tile indices into `MBATPF.GRA`, RLE: `w & 8000` → `(w&7FFF)` literal words follow; otherwise repeat next word `w` times. Runs are row-local. Decoder `0279` **[verified]**: all 19 maps decode to exactly their byte length. |
| `PFM_n` | 40×25 terrain/ownership codes: `1`/`2`/`3` land of player 1/2/3, `4` open sea, `5` coast/river, `9` house, `A` castle, `B` special water object (routine `46C0`, unidentified). |
| `PFC_n` | 40×25 terrain shade 1–8, stored in the low nibble of the owner grid. |
| `PFI_n` | 320×200 battle-view background. Elevation bands use indices 1–6 and get their colours from the battle palette. |
| `SHIPPAL` | Palette used in the battle view (1P). |
| Full screens (`TITLE0`, `CONTROLS`, `GAMETITLE`, `ISLSELECT`, `FIN_SCREEN`, …) | Raw 320×200. `GILSCREEN`/`P1DEADSCR` have a 10-byte header (`u32 size`, `u16 ?`, `u16 h=200`, `u16 w=320`). |
| Screen tilemaps (`HSC*`, `BRICKBACK`, `MAINTITLE`, `CFG*_SCR`) | 40×25 u16 tiles, no header. |
| Boxes (`TMSG*`, `BLCONQ`, `HOMESEL`, `RECRUITBOX`, …) | `u8 w`, `u8 h`, then w×h u16 tiles (MTRNPF / MTITPF tiles). |
| `SW_*` | 320×40 raw phase banners ("PLACE CANNONS", "BUILD AND REPAIR", "FINAL BATTLE"…). |
| `SOUND.RSC` | 37 Creative Voice (`.VOC`) effects. |
| `RMUSIC.RSC` | 7 songs × 3 variants (`XMI_` FM, `RXMI_` Roland, `SXMI_` PC speaker) as XMIDI, plus 9 Miles AIL drivers (`ADV_*`). `RAMP.AD` is the AdLib timbre bank. |
| `RAMPART.CFG` | 98 bytes XOR `"FE_FI_FO_FUM"` + `u32` checksum (sum of plaintext u32s). Holds controller assignments, key bindings (kbd1 @ +0x32, kbd2 @ +0x4C, 7 bytes each at stride 2), and options. |
| `RAMPART.HIS` | 216 bytes XOR `"@HI32N4"` + `u32` checksum. 3 boards × 9 entries × {`char initials[3]`, `u8 0`, `u32 score`}. |
| `XLOGO2` | Run-length sprite stream (`FC` skip / `FE n` literals); not decoded yet. |
| `PAUSE.STR` | 98303 bytes, stone texture for the pause scroll; layout not decoded yet. |

---------------------------------------------------------------------------------------------

## 4. Runtime data structures (DS offsets)

**Grids [verified]:** 42×27 bytes (the 40×25 playfield plus a 1-cell border), with cell
index `si = y*42 + x`.

- `35BB` **type grid**. Low 5 bits are the kind and bits 6–7 are the owning player (1–3).
  - `00` empty land
  - `01` wall
  - `02` castle (top-left of 2×2), with `0F` filling the rest of any 2×2 object
  - `03` destroyed wall / crater, cleared at the next rebuild
  - `05` sea; `06` coast/river (unbuildable); `07` border
  - `08` cannon (2×2) **[code]**
  - `09` ship (2×2, 1P)
  - `0A` sunk-ship water → back to sea at rebuild
  - `0B` object from the `72F4` list **[guess]**
  - `0C` house (buildable over)
  - `0D`/`0E` debris that blocks building for 3 rebuilds
  - `0B` grunt (list `DS:6629`, 4 bytes each)
- `0C` map house (PFM `9`): enclosing one during rebuild pays the area bonus
- `12` balloon (2×2)
- `13` Enhanced-mode house: spawned randomly each round on players' land (`4229`); building over it spawns a grunt; shooting it gives +5 wall hits
- `3A29` **owner grid**: bits 6–7 = player, bit 5 = enclosed, bit 4 = wall, bits 0–3 = terrain shade (8 = water).
- `3E97` u16 per cell: debris age / object pointer.

**Players:** 3 structs × 0x69 bytes at `231E`, indexed by `si` = 0, 0x69, 0xD2.
- `+0` (`231E`): flags. `8000` = active; bits 0–1 = colour index.
- `+0x0D` (`232B`): score (u32).
- `+0x1A` (`2338`): cannons allowed this round.
- `+0x2B` (`2349`): low byte = current piece / cannons left to place; high byte = rotation.
- `+0x1C` (`233A`): cannon/ship hits this round.
- `+0x1E` (`233C`): wall hits this round.
- `+0x47`/`+0x48` (`2365`/`2366`): cursor x/y in cells.
- `+0x59` (`2377`): territory points this round.

**Lists:**
- Castles `50D6`: 6 bytes each.
  - Flags: `80` = exists, `40` = disabled (Enhanced), `20` = home castle, `10` = enclosed, bits 0–1 = owner.
  - Then a state byte, x×8 (u16) and y×8 (u16).
- Cannons `56AB`…`5C4A`: 90 × 16 bytes.
  - Flags: `80` = alive, `10` = usable, `08` = destroyed, `04` = ball in flight, bits 0–1 = owner.
  - +1 x, +3 y, +0D facing (0–7), +0E hit points.
- Cannonballs `5C9F`: 40 × 19 bytes.
  - Flags, x, y, z (1/64 px fixed point), vx, vy, vz, target x/y, owning cannon pointer.
- Timer `615A` (ticks) / `615B` (seconds).
  - One tick per frame, 60 ticks per timer-second, so a displayed second is 61 frames ≈ 0.87 s.

---------------------------------------------------------------------------------------------

## 5. Game flow

`main 0238` → `title_sequence C3D8` (attract script; any key exits) → main menu: F1 Declare
War, F2 Game Options, F3 Configuration, F10 DOS → join screen (Blue / Red / Orange, "press
button to start", countdown) → `game_loop 0A20`.

**Options → constants [code]:**
- Battle Length 3/5/8/12/"to the death" → round limit `2601` = 3/5/8/12/100.
- Cannon Kill 3/6/9/12 shots → cannon HP `24CC` = 2/5/8/11. A cannon dies when HP goes
  below 0, so after HP+1 hits. Single player uses 12.
- Difficulty indexes the rebuild-time table.
- Game Type is Classic or Enhanced (`6C71`). Enhanced adds castle disabling and balloon/supply
  modules (not yet analysed).

**Single player** picks Recruit (beginner) or Veteran (+5000 points) and fights ships.
**Multiplayer** starts on a random map, then cycles through the 6 maps.

**Round (`play_round 0B55`)** [verified timings]:

| Phase | Routine | Duration |
|---|---|---|
| Select home castle (first round) | `479C` | 14 timer-s; the starting wall is auto-built |
| Place cannons | `2CE0` | 15 timer-s, or until every player has placed all of theirs |
| Battle: READY → AIM → fire | `4314` | 1 s+40 ticks, 1 s+20 ticks, then 11 timer-s |
| Pre-rebuild cleanup | `28C7` / `28E9` | prunes walls, ages debris |
| Rebuild | `1520` | 25/20/18/15 timer-s by difficulty, + 3 s grace |
| Score tally | `6994` | |

A player who encloses nothing during rebuild is defeated **[verified for 1P]**; the exact
condition (any castle vs. home castle) is not yet read from code. In 1P you get a
"continue with more firepower" option.

---------------------------------------------------------------------------------------------

## 6. Rules and mechanics

**Wall pieces [verified]:** 13 shapes × 4 rotations, each a 3×3 grid, at `DS:513F + (piece*4+rot)*9`:

```
0 single   1 domino   2 I3   3 small L   4 J   5 L   6 S   7 Z   8 T   9 U   10 S5   11 Z5   12 plus
```

A piece is legal only if every filled cell is type `00`, `0C` or `13`. Placing it sets the
cell to `owner|01`, sets wall bit `10`, and retiles the 4 neighbours (`217A`, mask N2 S8 W4
E1).

**Territory / enclosure [verified with 7 emulator tests, including 4 randomised]:**
`tools/enclosure.py` is an exact port and matches the game cell for cell.

The game doesn't flood-fill. It traces contours:
- Take `B = owner & 0x30` (wall or already enclosed).
- Scan in raster order for a solid cell whose west neighbour is empty.
- Walk the contour using a 4-direction pivot rule while summing left/right turns.
- If the turn total is negative, the contour encloses a hole. Re-walk it and, at each
  east-facing edge cell, fill eastward until reaching a marked west edge.

Consequences:
- **Walls must connect orthogonally. Diagonal-only corners leak.**
- Enclosed cells take ownership from the map's land owner, not from the builder.
- The search is time-sliced (96 steps per frame) and reruns continuously during rebuild.
- Enclosing a house during rebuild pays 100–1000 points by enclosed area, with thresholds
  9/16/25/36/49/64/81/100/121 cells.
- Enclosing an Enhanced house (`13`): in 1P adds 100, 150, 200… territory points; in multiplayer it counts toward sending grunts across the river (`7783`).

**Before each battle [code]:** one pass removes walls with ≤1 wall neighbour (dead-end
stubs), working on a snapshot so only the tips go. A cell whose 4 neighbours are all
wall/enclosed becomes enclosed.

**Cannon allowance (`0FCF`) [verified for 1P round 1 = 3]:**
- +1 per enclosed castle, and the home castle counts 2.
- First-round bonus: `clamp(sum/2 − sum/8 − 2, 1, 4)`, where *sum* is the cannons held by
  players already in the game (so late joiners catch up). The bonus is 2 in some 1P cases.
- Cannons are 2×2 and must go inside your territory.

**Cannon fire [code; firing verified]:**
- Each fire press launches from the next ready cannon, round-robin. A cannon is ready if it's
  alive, inside territory, and has no ball in flight.
- Speed is 64/80/96 sub-pixels per frame (1/1.25/1.5 px), indexed by a per-player level.
- Flight frames = isqrt(dx²+dy²) / speed. Velocity aims straight at the target.
  `vz = −z/frames + frames/2` with gravity 1/frame, so the ball lands exactly on the
  crosshair. A whistle sound plays at the apex.
- The muzzle offset comes from an 8-direction table.
- At most 40 balls can be in the air.

**Impacts (`366F`) [code]:**
- Wall → type 03 (crater), and the shooter gets +1 wall hit (+1 more if the wall had 3 neighbours).
- Cannon → HP −1. At <0 it's destroyed, and the shooter gets +1 cannon hit.
- Ship → ship HP −1, and the shooter gets +1 cannon hit.
- Sea → splash.
- Type `13` → becomes `03`, and the shooter gets +5 wall hits.
- Specially flagged balls leave `0E` debris instead of a crater.

**Scoring per round (`6994`) [code]:**
- Battle line: `(cannon_or_ship_hits*8 + wall_hits) * 2`, i.e. 16 per cannon/ship hit and 2 per wall.
- Territory line: points from enclosed cells and houses.
- Castle line: `[0, 500, 700, 900, 1000, 1200, 1400, 1600][castles] + bonus`.
- Scores are 32-bit.

---------------------------------------------------------------------------------------------

## 7. Not yet analysed

- Ship movement/AI details and HP values (ship table `DS:64E9`, 13 bytes each, 16 ships).
- Enhanced mode: the `supply` module and castle disabling.
- Map code `B`.
- Per-player cannonball "level" progression (`DS:4791`).
- Cursor speeds, rotation rules, and piece randomisation during rebuild (`16DE`).
- `XLOGO2` and `PAUSE.STR` layouts. Music needs an XMIDI → MIDI converter (data is standard
  XMIDI).

---------------------------------------------------------------------------------------------

## 8. Standalone Windows player (`player/`)

A Go port of the emulator core packaged as `Rampart.exe`, with the game files embedded via
`go:embed`. It has no dependencies beyond the Go standard library.

- **Core:** `cpu.go`, `machine.go` and `vfs.go`. It is pixel-identical to `emu/emu.c` over
  80 checkpoints in 8000 frames of play.
- **Sound:** `sb.go` emulates a Sound Blaster 2.0 (DSP 2.01, port 220, IRQ/DMA follow the
  game's setup screen, default 7/1) plus 8237 DMA; `opl.go` emulates the OPL2 FM chip.
  Sound-port I/O costs about 1 µs of emulated time, so the drivers' detection loops work.
- **Default config:** the embedded `RAMPART.CFG` selects card 3 (Sound Blaster), IRQ 7,
  DMA 1, DRQ 1. Config byte 0 is the card (0 none, 1 PC speaker, 2 AdLib, 3 SB, 4 SB Pro,
  5 PAS, 6 old PAS, 7 MT-32, 8–10 MT-32 + digital). Bytes 2/4/6 hold the IRQ, DMA and DRQ
  passed to the digital driver.
- **Front end (`win.go`, `audio_windows.go`):** a Win32 window via syscall (GDI
  StretchDIBits), raw scancodes, 70 Hz pacing, Alt+Enter/F11 fullscreen, and waveOut audio.
  Saves go to `%APPDATA%\Rampart`.
- **Headless runner (`headless.go`):** the non-Windows build; the same script format as
  `emu`. Set `RAMPART_WAV=file.wav` to record audio.
- **Build:**
  `cd player && GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H windowsgui -s -w" -o Rampart.exe .`
  The icon comes from `tools/make_icon.py` → `rsrc_windows_amd64.syso`.

---------------------------------------------------------------------------------------------

## 9. Enhanced mode, grunts, balloons, Veteran [code; balloon flows verified in emulator]

**Cannon-phase items (`2E74`/`2FD6`).** Rotate cycles item `[player+0x234B]`. The only gate
is `cmp [6C71],0` (Enhanced) at `2F8B`; there is no player-count check.

| Item | Cost (cannons) | Size | Notes |
|---|---|---|---|
| 0 cannon | 1 | 2×2 | `333E` with owner = player |
| 1 super cannon | 4 | 3×3 | flag `20` on the cannon. Its shots carry flag `08`: they leave `0E` burning debris (blocks building for 3 rebuilds), sink a ship in one hit, and the cannon needs two balloons to convert |
| 2 balloon | 3 | 2×2 | grid type `12` |

With exactly 3 cannons available, rotate goes straight to the balloon; with 4 or more, the
super cannon comes first.

The super cannon is placed only if all nine cells are enclosed empty land (low 5 bits of the
type `00` or `0C`; check `3382`). The first-round 6×6 castle yard, with the 2×2 keep in the
middle, has no free 3×3 spot, so super cannons need a wider enclosure. [verified]

**Balloons (`50B8`, run at the start of every battle in every mode).** Each balloon lifts off,
flies to a target picked by `542A`, hovers, and drops leaflets.
- **Multiplayer:** the target enemy cannon switches to the balloon owner's color for the
  battle. A super cannon needs two balloons: the first only marks it (`+0F` bit 0).
- **Single player:** the target is a ship. It's flagged converted (`64F5|=1`), and its gun
  becomes usable by the player, who fires it with their own fire button. **So balloons
  already exist in single player when the game type is Enhanced.**

**Grunts.**
- **Landing (`7362`):** troop ships (ship type 1, not converted) drop grunts onto an empty
  land cell next to the ship, from a per-round quota `[67D8]`. Each grunt targets a player.
- **Movement (`7437`):** each tick one grunt steps to a random empty neighboring cell
  (random direction order); grunts wander rather than seek.
- **Killed by:** a cannonball (`7737`), or enclosure during rebuild (`1BBA`), which turns
  the cell into land.
- **Blocking:** grunts block wall and cannon placement.
- **Enhanced multiplayer (`7783`):** for each house a player enclosed this rebuild, a grunt
  appears on a river or shore cell bordering an opponent's land.

**Houses (Enhanced).** Spawned randomly on each player's land every round (`41DF` → `4229`).
A wall piece placed over one turns that cell into a grunt instead of a wall (`2101`/`212F`).

**Single-player campaign.**
- **Map:** the island map (`ISLSELECT`, `6701`) has six regions; you pick which to attack
  next. Conquered regions show your color's shield.
- **Veteran:** choosing a region other than the beginner one as your first gives +5000 points
  (`[6941]`, added on the castle-bonus line). It also starts the round counter at 2 and your
  level at 1, which drive ship counts and types (tables `DS:6446` Classic / `6491` Enhanced).

**Controllers / colors.**
- **Controller IDs:** 1 keyboard 1, 2 keyboard 2, 4 mouse (INT 33h fns 0, 2, 3, 4, 7, 8;
  positions 0–319 × 0–199, re-centred after each read), 5 and 6 joysticks.
- **Join screen (`10C1`):** fills slots `DS:25FE..2600` in press order.
- **Setup:** `125B` (1P), `12DC` (2P) and `13A8` (3P) build the player structs. The color is
  the struct index (struct at `0x69 × color`) and also the land-ownership code (color + 1).
  2P always uses Blue and Red.
- **Ship guns (`6566`):** created with owner bits 1 (Red). That's harmless when Red isn't
  playing.
- **Keyboard handler quirk:** it ignores Left Shift presses (scancode `2A`), so the default
  keyboard-2 fire key never worked.

**Sound-card table `DS:5440`** (config byte 0): 0 none, 1 PC speaker, 2 AdLib, 3 SB, 4 SB
Pro, 5 PAS, 6 old PAS, 7 MT-32, 8–10 MT-32 plus SB/SB Pro/PAS digital. The PC-speaker driver
plays only a few cues (for example the castle-select drums).

## 10. Player patches (`player/features.go`)

Hooks run when CS:IP reaches an address in segment 0:

| Address | Patch |
|---|---|
| `1199` | join: each controller takes its own color's slot (settings) |
| `125B` | solo game: records the chosen colour; the game itself runs as Blue (see below) |
| `12DC` | 2P with any two colors, plus land relabeling |
| `0A20` | game_loop entry (back at the menus): solo recolour off, colour-name tables restored |
| `2F8B` | rotate item cycle redone host-side: super cannons and balloons each follow their own setting (Enhanced only / all game types / never); jumps to `2FCA` (cursor redraw), `2FCF` (locked: beep) or `2FD5` (nothing allowed) |

`RAMPART.CFG` is rewritten on every load with the player's sound card and key bindings.
When the game saves its config, its bindings and card are adopted back into `settings.json`.

## 11. Windows display and config notes

- **RAMPART.CFG is never written to disk.** The game still needs its encrypted format (it XORs and
  checksums the file), so the player builds an encrypted copy in memory from `settings.json` on every
  load. When the game saves its config (after its own option/control screens, `7CF4` → `7D3E`), the
  write is caught in memory and decoded back into `settings.json`: sound card, both key sets,
  `game_options` (difficulty, game type, cannon kill, battle length = cfg words 45–48, DS:6C6F..6C75),
  and `game_cfg_words` (all 49 words, plain numbers). An encrypted `RAMPART.CFG` left in the save
  folder by an older build is imported once and deleted.
- **White window.** A window that never gets a frame on screen shows white under DWM. The player
  draws through WM_PAINT with a DIB section + StretchBlt, samples the window with `GetPixel` for the
  first ~2000 frames, and falls back to StretchDIBits, then SetDIBitsToDevice, if frames don't arrive.
  At startup it removes Program Compatibility Assistant entries for its own exe path
  (HKCU `AppCompatFlags\Layers`, `Compatibility Assistant\Store`/`Persisted`) and, if a
  compatibility layer was active (`__COMPAT_LAYER`), restarts itself once without it. The embedded
  manifest (asInvoker, supportedOS Win7–11, dpiAware) keeps the assistant from adding new entries.
  `rampart.log` and `frame700.bmp` in the save folder record what happened.

## 12. Solo colour and the build-phase overtime

**Solo colour is cosmetic.** Single-player code assumes the human is Blue: `5728` gives every
inactive slot's struct to the ships (the last one, Orange, ends up in `67B9`), and the 1P
palettes (`M1PPF PALETTE`, `SHIPPAL`) reuse the Red and Orange player ranges for fire and ship
colours (`113`–`115` cycle). Moving the human into the Red or Orange struct therefore drew the
castle with fire colours ("disco castle") and broke the ships' targeting. Now the game always
runs as Blue and only the display changes:
- Player graphics use per-colour palette bases at `DS:477B` (6/7/8 → DAC 96/112/128: walls,
  territory, cursors, boxes, shields) and `DS:477E` (0D/0E/0F → DAC 208/224/240: castle keeps).
  All multiplayer palettes share the same three 16-entry blocks, so the player shows Blue's
  blocks recoloured to the chosen colour's blocks (taken from `M2PPF PALETTE`), following fades
  by matching the block at any brightness.
- Colour-named graphics come from two name tables, `DS:EC0B` (boxes: XXCONQ, XXDEF, XXSCORE,
  XXCONT…) and `DS:68C9` (XXSHIELD), each pointing at "BL"/"RD"/"OR". In a solo game the BL and
  chosen colour's strings are swapped.

**Build-phase overtime [code + verified].** The rebuild timer is frame-based (`timer_tick`
`46E8`: 61 frames per timer second). At 0 the game sets `47A9`, hides the timer display
(`6165`) and gives 3 more timer seconds. During those you can still place the piece you're
holding, but no new piece is dealt (`2333`, the 30-frame next-piece delay, is only set when
`47A9` = 0). Stock behaviour, identical with all patches off.

## 13. Castle colours and game rules (F12 menu)

**Castle colours** (session only, `colors.go`): per side, a preset (Purple, Green, Pink, Teal,
Gold, Cyan, White, Black, Rainbow, Disco) recolours that side's palette blocks on screen. Entries
that differ between the Blue/Red/Orange reference blocks are the side's own colours; they get the
preset hue with their original lightness/saturation, shared entries (grass, black) stay. Fire uses the 1P palette at Red's blocks (`M1PPF PALETTE` 112/224), rotating entries 1–3 every 8 frames like the game does in battle: the original "disco castle". In
single player only the Blue blocks are touched (the others hold fire/ship colours).

**Game rules** (saved, `rules.go`), all verified in the emulator:

| Rule | Hook | Effect |
|---|---|---|
| Build / cannon / battle time | `154D` / `2D3A` / `4445` (AH before `timer_set`) | 25–300 %, 1–99 s |
| Extra cannons | `1052` (`[si+2338]` final) | −3…+10 per round, 1–30 |
| Cannon toughness | `0AED` (`[24CC]` set) | shots to kill × 25–300 % |
| Grunts (1P) | `5A77` quota `[67D8]`, `5AB5` landing countdown `[6844]` | none / normal / more ×2 / invasion ×4 |
| Ironman (1P) | `6EE4`: `[si+2343]` = 4 | first defeat is game over |
| Always endless (1P) | see section 14 | every 1P region is endless |

## 14. Endless mode [verified in emulator]

**How 1P regions end (stock).** After each battle, `6654` returns 1 (region conquered) if
`[64DC+level]−1 ≤ round` or `[643C+level] > enemy strength [67CD] − 7×cannons`. Round
limits by level: 4/9/18/20/22/5; thresholds 20/30/60/80/80/15. Separately, at `0C46` a
1P region also ends as conquered once round `[22D8]` > 12. Level `[si+2342]` goes up by one
per region (max 4); round resets per region. The ship planner clamps its round-indexed
lookups (`5619` to 4, `59AA` to rounds played `[si+2344]`, `60DC` random capped at 3), so
ship waves plateau within a region and only the level makes them grow.

**Ship pool [verified].** At region start `578A` fills a pool per ship type `[67D4..67D6]`
from `[644B/6496 + 15×difficulty + 2×level]` → 3 counts (Classic difficulty 0: 10, 18+6,
15+20, 13+26, 3+24+3 for levels 0–4) and sets `[67D7]=3`. Each round `59AA` adds a
release count (grows with round and rounds played `[si+2344]`) to `[67D7]`, caps it at the
pool total `[67D9]`, and draws that many ships into the spawn queue `[67D1..67D3]`. Every
`[67DC]` rounds one extra type-2 ship is added. When the pool is empty only that trickle
remains. A continue (`662B`) refills the pool via `578A`.

**Endless (`rules.go`).** Hook `6654`: return 0 (never conquered) and count a survived
round. Hook `0C46`: skip the 12-round cap. Hook `59AA`: e = rounds survived − 4 × continues
used; level = start level + e/4 (max 4); pool = min(12, 3 + (e+1)/2) ships in the level's
type mix, released in full (`[67D7]` = pool), and ships still queued from the last round
are dropped. Verified: 3, 4, 4, 5, 6 … ships per round; a continue steps back one level and
a ship.

**ENDLESS box (`endless.go`).** The island screen `6701` places RECRUITBOX on region 0
and VETERANBOX on region 1 or 2 (`681C`: bx = 4 or 8); each box is a selectable castle
for `home_castle_select`, and the chosen region (`68D9`) becomes the map `[24CD]`.
- Box resources (MTRANS.RSC) are 12×9 cells of tile ids into the tile bank `[1067]`,
  which holds MTRNPF.GRA (782 tiles) in a 1024-tile segment; ids 0x30E+ are free.
- At `6899` we write our tiles (gothic title drawn to match VETERAN, small font
  matching "ADVANCED LEVEL", colours 100/107) to ids 0x310+, build the box at DS:2975
  (scratch after the four loaded boxes), point `[6921]` at it and re-enter the game's
  own castle_add + draw code at `6824` with cx,dx = the free region of 1/2.
- At `4D58` (first cursor pick, bh=1) we select castle entry 0 (Recruit); otherwise the
  scan down the left edge would land on a box on region 1.
- At `68E7`, if our region was picked: Endless for this game, map = setting or random
  (0–6, 6 = the final map), skip the Veteran bonus (`6913`).

## 15. Music dropout fix and Yappy Captain [verified in emulator]

**Music stopping (emulator bug, fixed in 7.4).** Each phase loads its song with `2515`: it
stops and releases the old sequences, frees their DOS blocks (INT 21h/49h), loads the XMI
from RMUSIC.RSC and allocates a block per sequence (48h). The emulator's DOS allocator was
a bump pointer that ignored frees, so after a few rounds of song loads (round 2 in tests) allocation failed,
`2515` bailed out and no song was registered again: the OPL received no writes for the
rest of the session. `machine.go` now has a first-fit block allocator with free/resize.
Verified: music keeps playing through 16 rounds (60000 frames).

**Sound clips.** The digital driver loads only 20 of SOUND.RSC's 37 clips (name table
DS:F261): agony2, aim, baboom, blkhit1, cease, clunk1/2, dblclic, elechit1, exp2/3, explrg1,
final, fire, guill2, magnum1, ready, whoosh2, woodcrus, zap1. The arcade commander lines
(blubeg/redbeg/orgbeg, bluend/redend/orgend, alive, capture, rotate, placecan, welldone)
ship unused. `play_sfx 26B7` actually starts an XMI sequence of the current song; digital
clips go through `27DC` (DI = clip in segment 62A0).

**Yappy Captain (`captain.go`, F12 toggle).** Plays those lines, mixed on top of the
emulated card (`SoundBlaster.Voice`), queue of 2, linear resampling 8 kHz → 44.1 kHz.

| When | Hook | Line |
|---|---|---|
| cannon phase | `2CE0` | placecan |
| build phase, 70 frames after its timer starts (`154D`), no castle enclosed | frame hook | alive (MP: once per build phase if any human has none) |
| 1P build phase, castle enclosed at that check or later; once per build phase (grunts make enclosure flicker) | frame hook | capture + colour end |
| wall piece blocked | `20C6` (try_place_piece fail), 10 s cooldown per player | colour beg + rotate |
| 1P region / world conquered | `0D36` / `0DBC` | welldone + colour end |
| 1P game over, walk-the-plank picture (P1DEADSCR, loaded by `C1F2`) | `0DA5` | agony2 |

Enclosure: castle list DS:50D6 (x*8, y*8 → grid cell x/8+1, y/8+1); a castle is enclosed when
its type-grid cell (DS:35BB) has the owner code (colour+1) in bits 6–7. The 50D6 flag bits
0x10/0x20 are only set transiently by `count_cannon_castles`.

---------------------------------------------------------------------------------------------

## 16. Computer players and Smart Pieces (v8)

**Controller reads [code].** Every phase reads a player's input through
`7870 read_controller(al = controller id)` → `ax` buttons (1 fire, 2 rotate), `cx`/`dx` moves,
except the battle crosshair, which uses `7AD6` (same, in pixels; bit 8 doubles the step).
Ids 1/2 keyboards, 4 mouse, 5/6 joysticks; anything else reads as nothing. The caller is
recognisable by the return address:

| Return | Phase | Movement unit / state |
|---|---|---|
| `1729` | rebuild (`16FC`) | cells; piece = `[si+2349]` (13 = none), rotation `[si+234A]`, cursor `[si+2365/66]` = top-left of the 3×3 piece grid; the cursor may only sit where cell (x+1,y+1) is land (`2012`), so it can't cross rivers |
| `2E6C` | cannon placement | cells; `[2349]` low = cannons left, `0x100` = 15-frame lock; 2×2 cannon at the cursor, every cell type 00/0C with owner bit 0x20 (`2FEA`) |
| `4AE8` | home castle select | directions jump between castles (`4CEB`), fire picks |
| `4643` (`7AD6`) | battle aim | pixels `[si+236B/6D]`; fire launches from the next ready cannon (`3580`: alive, own colour, no ball in flight, usable) and lands exactly on the crosshair |
| `8EF3` | high-score initials | letter `[7ACC]`, position `[7ACE]`; a *negative* move steps the letter up |

Join screen: slots `25FE..2600` = Blue/Red/Orange, `[24CA]` players. `11DD` takes slot `si` for
controller `bl` and draws it (`C75B`, bx=25D9, ax=slot|100h); `C75B` with bx=25CF, ax=slot
draws it empty. `11FC` = countdown over.

**Bots (`bots.go`).** Keys 1/2/3 on the join screen (hook `1153`) put controller id 8/9/10
in a seat; a human joining that colour takes it over. Hooks on `7870`/`7AD6` answer reads
for ids 8–10 by phase. A lone bot is dropped at `11FC` (the 1P campaign needs a human).
Skill (F12 → Game Rules → Computer players) sets cursor/rotate pace, aim error, fire gap
and how far it expands. Bots enter "BOT" as initials. Captain lines skip bot seats.

**Planner (`planner.go`).** Cheapest orthogonal wall loop around a castle: Dijkstra over
(cell, parity of crossings of a ray going up from the keep), walls cost 0, own free land 1
(+2/+1 within 1/2 cells of the keep, so the yard keeps cannon room), grunts 4, else
impassable. First target: the cheapest castle (flag 0x20 preferred); once one is enclosed,
the next castle within the skill's reach. Placements score covered gap cells ×10, other
cells −3, cells inside an enclosure −8. Bots hold a piece that helps nothing rather than
litter the land. In battle they shoot walls on the loop around an enemy's enclosed castle (Hard prefers
the loop's corners) or the enemy's most damaged cannon, two shots at a time; the chance
of picking a cannon is 0.35 × min(1, 3 / hits it still needs), so tough cannons (Cannon
Kill 12) draw few shots (v8.2; before that a bot spent all HP+1 shots on one cannon).
"Enclosed" is decided by an actual closed wall loop (`loopAround` walls-only), not the
grid's enclosure marks. Once secure, bots always work toward the cheapest next castle
(v8.2; the old reach cap left Easy bots stuck in their first yard). Cannon phase (v8.3): every placement takes the reachable spot that leaves the most free
2×2 spots (tight packing; ties toward the enemy, whose land centre is nearest). If the
cannons won't all fit (`packCount` < cannons left), Normal and Hard spend 3 on a balloon;
Hard also places one super cannon a round when it has 4+ and a free 3×3 spot (5+ if the
rest would fit anyway). Items are picked by pressing rotate (host-side cycle, `2F8B`) and
only if that item is allowed by the Balloons / Super Cannons settings. A defeated bot presses
fire at the MP continue wait (`70FB` → `7864`, return `786D`) so the game doesn't sit out
the 15 s. MP defeat: `[si+2343]` < 2 gets a new castle, the third defeat removes the
player (`7079`: flags &= 7FFF).

**Smart Pieces (Game Rules).** Hook at `560E` (piece dealt: `al`, player `si`): Assist
replaces half the pieces, Exact all, with the piece whose best placement repairs the most of
that player's planned loop (−1 if nothing needs repair: keep the dealt piece). Off in single
player unless "Smart pieces (1 player)" is on. Unit tests: `planner_test.go`.

**Endless records (`leaderboard.go`, v8.1).** Top 10 Endless runs in `endless.json` (save
folder), ranked by battles survived, then score. Tracked host-side: rounds at the win check
(`6654`), score (`[si+232B]` u32) at each win check and defeat (`6EE4`), continues = defeats − 1,
ship level from the Endless planner. When the game is back at `game_loop` (`0A20`) after an
Endless run, the board opens over the screen (emulation paused) for initials, prefilled
with the initials typed on the game's own high-score screen if it came up (`8FED`: three
letters entered, buffer at `[7AC8]`), else the last name used. F12 → Endless Records shows it.

## 17. Versions and the game files this player needs

`player/version.go` holds the mod name (Rampart: Refortified), author (Shifting Dollars),
official source URL (github.com/SHIFTINGDOLLARS), `Version` (bump it every release; history is in its comment) and
the hashes of the embedded game files. The patches sit at fixed addresses in `RAMPART.EXE`,
so the player works only with this build of the game:

- `RAMPART.EXE`: 49,982 bytes, SHA-256 `05e14a4ed47d02b95608b17c6355f53db1fdb44c6d27619443c76c7b68eb2d17`
  (MD5 `389cf82a6bade7f4a1394821b142e725`), the English DOS release (`Rampart_DOS_EN`).
- The 19 other game files (all but `RAMPART.CFG` / `RAMPART.HIS`, which the player rewrites):
  SHA-256 over name, NUL, contents in name order = `532a44b5070f175c50424f557a50658dea43db06a73015ca18d1ae9912595aed`.

The same values are shown in F12 → About / Version, in the window title (version only), and
saved as `about.txt` in the save folder (with the release name: the credits scroll thanks
"HAL9000 from I.N.C for *UNPROTECTING* this fine Bitmasters game", and the exe carries "Crk by
HAL9000."; the About page shows it only when the exe hash matches), so a player can paste them when reporting a problem.
A bring-your-own-files build should compare against these at startup.

## 18. Gamepads (XInput, v8.4)

`pads.go` / `pads_windows.go`. Each frame the Windows front end calls `XInputGetState`
(xinput1_4, else 1_3, else 9_1_0) for slots 0–3 (a disconnected slot is re-checked every
~2 s, the call is slow) and hands the first two connected pads to `App.SetPad` as pad 1 / 2.
They play as the game's joystick controllers 5 and 6: the controller-read hooks (`7870`,
`7AD6`, shared with the bots) answer for those ids from the pad state, so the game's own
port-201h joystick code is never used (port 201h still reads "nothing").

- Fire/rotate are latched on the press edge and cleared when the game reads them, like the
  keyboard latches (bit 6 of `DS:2151[sc]`). The aim read also reports held buttons (bits 4/8;
  rotate held doubles the crosshair speed, as on the keyboard).
- Cell phases: a direction steps at once, repeats after 14 frames, then every 4 (2 at full
  tilt), scaled by PAD CURSOR SPEED. Battle: left stick with a dead zone of 7849 and a
  squared response, up to 4 px per frame; D-pad 1 px per frame.
- A / RB / right trigger fire; B / X / Y / LB / left trigger rotate; Back opens F12; Start is
  the game's Pause key (scancode 45h, checked in `0594`) during a game (from `play_round
  0B55` until `game_loop 0A20`), F1 otherwise. In the F12 menu and the records screen the pad
  sends the menu keys (D-pad, A = Enter, B / Start = Esc).
- Join: pads use the colour from F12 → Controls (PAD 1 / PAD 2 PLAYS), or the next free seat
  with "as joined" (default).
- Headless: `pad <0|1> <buttons hex> [lx ly lt rt]` (buttons −1 = disconnected). Unit
  tests in `pads_test.go`.

## 19. Grunt buster (v8.5, `gruntcannon.go`)

A fourth cannon-phase item: rotate cycles cannon → super cannon → balloon → grunt buster
(each only when its setting allows it and enough cannons are left; the buster costs 2).
F12 → Game Rules → Special Cannons: 1 player only (default) / all games / never.

I created this because I was tired of grunts in Endless mode. 
This cannon shoots 5 cannon balls at once. 
it can only fire on grunt-occupied land so that it doesn't expend the cannonball limit 
unless it needs to.
It's not broken, it just needs to meet certain conditions to actually fire.

- **Placement** goes through the plain cannon path (`2FD6` sends items other than 1/2 to
  `2FEA`, which calls create_cannon `333E`). `2FEA` notes the item; at `336D` (entry
  filled, si = cannon) bit 7 of cannon byte +0F marks a buster (bit 0 is the balloon mark,
  the rest of +0F is otherwise unused; we clear bit 7 on every new cannon because
  create_cannon doesn't touch +0F). The cost (`2EE7`, which only charges items 0–2) is
  done host-side: −2, lock 15 frames, item back to 0, then `2F44` (cursor redraw).
  Cursor shape `be39` only knows items 0–2: at `BE8D` item 3 gets the cannon cursor.
- **Firing.** `player_aim` calls next_ready_cannon at `46AA`; we replace that call with the
  same round-robin (pointer `DS:5F9B + 2×colour`; ready = alive, own colour, no ball in
  flight or destroyed bits (0C), usable 10) but pick a buster only when a grunt (type 0B)
  is on the crosshair cell or a neighbour, and skip busters otherwise.
- **Five balls.** At `46BB` (call launch_cannonball: si cannon, ax colour/flag, cx,dx
  target) a buster's call is repeated for the + offsets (±8 px) by pushing `46BB` as the
  return address and jumping to `3467`; the last one runs the original call. Each ball
  records its cannon at ball +11h.
- **Landing.** update_cannonballs calls ball_impact at `364F` with bx = the ball's cannon.
  For a buster we enter ball_impact past its prologue (same five pushes, `[5FB1]`−1) at
  its grunt branch `3704` (si = the cell) if the cell holds a grunt, else at the "nothing"
  puff `3746`. So its balls never hit walls, cannons, ships or houses.
- **Looks** are display-only (`Machine.IdxFix`, run on the indexed frame before the
  palette): building-view cannons are 2×2 tiles (`DS:8378 + 4×level`, row stride 10h,
  bank `[1061]`) drawn in mode 20h, i.e. pixel = player block (`[477B+colour]`<<4) | low
  nibble; battle-view cannons are 2×3 pre-coloured tiles `28h + 36h×colour + 6×facing`
  from segment `[89CC]`, drawn 24 px above the cannon's map position (`A8F2`). Where the
  frame still shows a buster's tiles, barrel nibbles 6/8/A/B become black (2), its
  highlight 4 dark grey (D), and black pixels between barrel pixels (the bands, the
  muzzle) the player's colour (5). The placement cursor is a separate sprite in the cursor
  colours (D1–D4 cycle); in buster mode D5/D6/D9–DB become black and D7/D8 the player's
  colour. Battle view (v8.8): the plain cannon turned neutral silver in RGB after the palette
  (`Machine.RGBFix`: saturation 0, lightness 0.35 + 0.75×l). A8F2 puts the 2×3 battle
  sprite (tiles `28h + 36h×colour + 6×facing` + 0..5 from segment `[89CC]`) on the
  cannon's two columns, one row above it to one row below, each tile at its cell's flat
  map position, so the sprite sits exactly 8 px above the cannon's (x,y). (v8.6–8.7
  searched for it at different heights and could match an identical neighbour.) The
  sprite tiles also carry the floor pattern (block colours 1 and 2): those pixels are
  neither matched nor tinted. Sprites are copied into a cache only while the game is
  drawing cannons (draw-object `959C` with ah = 2, battle `A8F2`; at A8F2 al = facing,
  si = cell, and the cached battle sprite is re-copied whenever the bank's tiles differ:
  the battle bank holds the current firepower level's look and is reloaded after a
  continue, v8.9): between phases the
  tile banks are reused for other graphics while the old picture is still on screen, and
  copying then cached garbage. The building view tries all three cannon looks
  (`8378 + 4×level`): the level byte `[4791+colour]` is read for whichever colour the game
  is handling at draw time.
- Tests: `RAMPART_FORTRESS=<frame>` keeps the walls standing at that frame,
  `RAMPART_AIMGRUNT=1` points player 1's crosshair at a grunt (solo).

**Cannon looks / level [code].** `[4791+colour]` picks the cannon's look (tables `8358`/`8378`
+ 4×level, battle tables `BD86`/`BDF2`/`BE5E`) and its ball speed (`5C75`: 64/80/96).
It isn't random: it is reset to 0 for a new game (`0EE3`, `1488`), +1 (max 2) when a
single-player continue is taken (`6F8B`, "continue with more firepower") and when a
multiplayer player is defeated and keeps playing (`71DD`); `5D9F` (ship code, not fully
read) also sets it from ship state and rounds played.
You generally get three lives in this game.
Continuing with more firepower improves the cannon from:
level 0 (banded cannons) level 1 (artillery cannons) and level 2 (muzzled artillery)

## 20. Release package (bring your own files), v9.0

- Two builds. Default: `game/` is embedded (`gamefs_embed.go`, personal use only).
  Release: `-tags byof` (`gamefs_byof.go`) compiles in no game data and loads the player's
  own copy at startup.
- Search order: command-line folder (or a dropped RAMPART.EXE), `RAMPART_GAME`, the folder
  saved in `%APPDATA%\Rampart\gamedir.txt`, then the exe's folder and subfolders two levels
  deep. File names match case-insensitively.
- Every file except RAMPART.CFG/HIS must match its SHA-256 (`wantFiles`); the error names
  missing and differing files. No CFG: a default is built from `defaultCfgHex`. No HIS: the
  game creates one in the save folder on the first high score (tested).
- Verified: attract-mode frames 500..9000 are byte-identical between the two builds; a full
  3-bot game runs to the initials entry; the release exe contains none of the game files.
- Build the release:
  `cd player && GOOS=windows GOARCH=amd64 go build -tags byof -trimpath -ldflags "-H windowsgui -s -w" -o ../dist/release/Rampart.exe .`
- The source package leaves out `player/game/`, `orig/`, `work/` and `out/` (the disassembly).

package main

import "testing"

// Shape table as dumped from DS:513F.
var testShapeBytes = []byte{0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 1, 1, 0, 0, 0, 0, 0, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 1, 0, 0, 0, 0, 1, 1, 1, 0, 0, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 0, 0, 1, 1, 1, 0, 0, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 0, 0, 0, 1, 1, 0, 1, 0, 0, 0, 0, 1, 1, 0, 0, 1, 0, 0, 1, 0, 1, 1, 0, 0, 0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 0, 0, 0, 0, 1, 1, 1, 0, 0, 1, 0, 1, 0, 0, 1, 0, 1, 1, 0, 1, 0, 0, 1, 1, 1, 0, 0, 0, 0, 1, 1, 0, 1, 0, 0, 1, 0, 0, 0, 0, 1, 1, 1, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 1, 0, 0, 0, 1, 1, 1, 1, 0, 0, 0, 0, 1, 0, 0, 1, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1, 1, 1, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 1, 1, 1, 1, 0, 0, 0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0, 1, 1, 0, 0, 1, 1, 0, 1, 0, 1, 1, 0, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 0, 0, 0, 1, 0, 1, 1, 0, 1, 0, 0, 0, 0, 1, 1, 1, 0, 1, 0, 0, 1, 0, 1, 1, 0, 0, 1, 0, 0, 1, 0, 1, 1, 1, 0, 0, 0, 0, 1, 0, 0, 1, 1, 0, 1, 0, 0, 0, 0, 1, 1, 1, 1, 0, 1, 1, 1, 0, 0, 1, 0, 1, 1, 0, 1, 0, 1, 1, 1, 1, 0, 0, 0, 0, 1, 1, 0, 1, 0, 0, 1, 1, 1, 0, 0, 1, 1, 1, 0, 0, 1, 0, 1, 1, 0, 1, 0, 1, 1, 0, 1, 0, 0, 1, 1, 1, 0, 0, 1, 0, 1, 1, 0, 1, 0, 1, 1, 0, 0, 0, 1, 1, 1, 1, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 1, 1, 1, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 1, 1, 0, 1, 0, 1, 1, 1, 0, 1, 0, 0, 1, 0, 1, 1, 1, 0, 1, 0, 0, 1, 0, 1, 1, 1, 0, 1, 0, 0, 1, 0, 1, 1, 1, 0, 1, 0}

func testBoard() *board {
	loadTestShapes()
	b := &board{}
	for i := 0; i < gn; i++ {
		x, y := i%gw, i/gw
		if x == 0 || y == 0 || x == gw-1 || y == gh-1 {
			b.t[i] = 7
			continue
		}
		b.o[i] = 1 << 6 // land of player 1
	}
	c := castleInfo{x: 20, y: 12, home: true}
	b.castles = []castleInfo{c}
	for _, i := range []int{c.y*gw + c.x, c.y*gw + c.x + 1, (c.y+1)*gw + c.x, (c.y+1)*gw + c.x + 1} {
		b.t[i] = 0x0f
	}
	b.t[c.y*gw+c.x] = 0x02
	// ring at distance 3: x 17..24, y 9..16
	for x := 17; x <= 24; x++ {
		b.t[9*gw+x], b.t[16*gw+x] = 1, 1
	}
	for y := 9; y <= 16; y++ {
		b.t[y*gw+17], b.t[y*gw+24] = 1, 1
	}
	return b
}

func loadTestShapes() {
	for p := 0; p < 13; p++ {
		for r := 0; r < 4; r++ {
			pieceShapes[p][r] = nil
			for k := 0; k < 9; k++ {
				if testShapeBytes[(p*4+r)*9+k] != 0 {
					pieceShapes[p][r] = append(pieceShapes[p][r], [2]int{k / 3, k % 3})
				}
			}
		}
	}
	shapesLoaded = true
}

func TestLoopClosed(t *testing.T) {
	b := testBoard()
	l := b.loopAround(b.castles[0], 1, true)
	if l.cost != 0 || len(l.cells) != 28 {
		t.Fatalf("closed ring: cost %d, %d cells", l.cost, len(l.cells))
	}
	if pl := b.planFor(1); pl.nNeed != 0 {
		t.Fatalf("closed ring needs %d", pl.nNeed)
	}
}

func TestCornerGap(t *testing.T) {
	b := testBoard()
	b.t[9*gw+17] = 0 // knock out the top-left corner
	pl := b.planFor(1)
	if pl.nNeed != 1 || !pl.need[9*gw+17] {
		t.Fatalf("corner plan: need %d", pl.nNeed)
	}
	if p := b.smartPiece(1); p != 0 {
		t.Fatalf("corner: smart piece %d, want 0 (single)", p)
	}
}

func TestStraightGap(t *testing.T) {
	b := testBoard()
	for x := 19; x <= 21; x++ {
		b.t[16*gw+x] = 3 // craters can't be built on...
		b.t[16*gw+x] = 0 // ...but cleared land can
	}
	pl := b.planFor(1)
	if pl.nNeed != 3 {
		t.Fatalf("straight plan: need %d", pl.nNeed)
	}
	if p := b.smartPiece(1); p != 2 {
		t.Fatalf("straight gap: smart piece %d, want 2 (I3)", p)
	}
}

func TestDiagonalLeaks(t *testing.T) {
	b := testBoard()
	// replace the top-left corner by a diagonal step: not enclosed
	b.t[9*gw+17] = 0
	b.t[10*gw+17] = 0
	b.t[9*gw+18] = 0
	b.t[10*gw+18] = 1
	if l := b.loopAround(b.castles[0], 1, true); l.cost == 0 {
		t.Fatal("diagonal corner counted as closed")
	}
}

func TestFromScratch(t *testing.T) {
	b := testBoard()
	for i := range b.t {
		if b.t[i] == 1 {
			b.t[i] = 0
		}
	}
	l := b.loopAround(b.castles[0], 1, false)
	in := interior(l.cells)
	n := 0
	for _, v := range in {
		if v {
			n++
		}
	}
	if n < 36 {
		t.Fatalf("fresh ring encloses only %d cells (cost %d)", n, l.cost)
	}
}

func BenchmarkSmartPiece(bm *testing.B) {
	b := testBoard()
	for i := range b.t {
		if b.t[i] == 1 && i%3 == 0 {
			b.t[i] = 0
		}
	}
	b.castles = append(b.castles, castleInfo{x: 6, y: 5}, castleInfo{x: 32, y: 20}, castleInfo{x: 8, y: 20})
	for i := 0; i < bm.N; i++ {
		b.smartPiece(1)
	}
}

func BenchmarkBotPlan(bm *testing.B) {
	b := testBoard()
	b.castles = append(b.castles, castleInfo{x: 6, y: 5}, castleInfo{x: 32, y: 20}, castleInfo{x: 8, y: 20})
	for i := 0; i < bm.N; i++ {
		pl := b.planReach(1, 48)
		b.bestPlacement(pl, []int{5}, b.cursorBFS(20, 20))
	}
}

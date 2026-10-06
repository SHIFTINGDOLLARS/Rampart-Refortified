package main

import "math/rand"

// Wall planning, shared by the bots and Smart Pieces.
//
// A castle is enclosed when a closed, orthogonally connected loop of wall cells
// surrounds it (diagonal-only corners leak, see NOTES.md section 6). The planner
// finds the cheapest such loop: existing walls cost nothing, free land of the
// player costs 1 (more right next to the keep, so the yard keeps room for
// cannons), everything else is impassable. The empty cells on that loop are
// what still has to be built. To force the loop around the castle it uses the
// usual ray trick: a ray goes up from the keep, and a path only closes into a
// loop around the keep if it crosses that ray an odd number of times.

const (
	gw, gh = 42, 27
	gn     = gw * gh
	inf    = 1 << 20
)

type castleInfo struct {
	x, y int  // top-left cell of the 2x2 keep
	home bool // castle flag 0x20: set by count_cannon_castles for castles that
	// counted last time (the home castle among them); a mild preference only
}

type board struct {
	t, o    [gn]uint8 // type grid DS:35BB, owner grid DS:3A29
	castles []castleInfo
}

func readBoard(m *Machine) *board {
	b := &board{}
	for i := 0; i < gn; i++ {
		b.t[i] = m.dsb(0x35bb + uint32(i))
		b.o[i] = m.dsb(0x3a29 + uint32(i))
	}
	for o := uint32(0); o < 6*64 && m.dsb(0x50d6+o)&0x80 != 0; o += 6 {
		x := int(m.dsw(0x50d8+o))/8 + 1
		y := int(m.dsw(0x50da+o))/8 + 1
		if x > 0 && y > 0 && x+1 < gw && y+1 < gh {
			b.castles = append(b.castles, castleInfo{x, y, m.dsb(0x50d6+o)&0x20 != 0})
		}
	}
	return b
}

func (b *board) kind(i int) uint8 { return b.t[i] & 0x1f }
func (b *board) land(i int) int   { return int(b.o[i] >> 6) }
func (b *board) wall(i int) bool  { return b.kind(i) == 1 }
func (b *board) free(i int) bool  { k := b.kind(i); return k == 0 || k == 0x0c }

// cursorOK: the game keeps a cursor at (x,y) only if cell (x+1,y+1) is land (2012).
func (b *board) cursorOK(x, y int) bool {
	if x < 0 || y < 0 || x+1 >= gw || y+1 >= gh {
		return false
	}
	return b.o[(y+1)*gw+x+1]&0xc0 != 0
}

// enclosedBy reports whether the castle's keep cell is enclosed land of code.
func (b *board) enclosedBy(c castleInfo, code int) bool {
	return int(b.t[c.y*gw+c.x]>>6) == code
}

// ------------------------------------------------------------- piece shapes

// pieceShapes[p][r] = filled cell offsets (row, col) inside the 3x3 grid,
// read from DS:513F (13 pieces x 4 rotations x 9 bytes).
var pieceShapes [13][4][][2]int
var shapesLoaded bool

func loadShapes(m *Machine) {
	if shapesLoaded {
		return
	}
	for p := 0; p < 13; p++ {
		for r := 0; r < 4; r++ {
			var cells [][2]int
			for k := 0; k < 9; k++ {
				if m.dsb(0x513f+uint32((p*4+r)*9+k)) != 0 {
					cells = append(cells, [2]int{k / 3, k % 3})
				}
			}
			pieceShapes[p][r] = cells
		}
	}
	shapesLoaded = true
}

// ------------------------------------------------------------- loops

type loopResult struct {
	cost  int
	cells []int // loop cells in order (closed: last connects to first)
}

// cellCost for building a loop around castle c for land code `code`.
// wallsOnly: only existing walls may be used (finds the loop that encloses it now).
func (b *board) cellCost(i, code int, c castleInfo, wallsOnly bool) int {
	k := b.kind(i)
	if k == 1 {
		return 0
	}
	if wallsOnly || b.land(i) != code {
		return inf
	}
	x, y := i%gw, i/gw
	d := max(c.x-x, x-(c.x+1), c.y-y, y-(c.y+1), 0)
	extra := 0
	if d <= 2 {
		extra = 3 - d // keep the yard roomy: d=1 costs 3, d=2 costs 2
	}
	switch k {
	case 0, 0x0c:
		return 1 + extra
	case 0x0b: // grunt: it may wander off
		return 4 + extra // max cell cost 7: fits the 16-bucket queue
	}
	return inf
}

// loopAround finds the cheapest wall loop enclosing castle c (cost inf: none).
// Dijkstra over (cell, ray parity) states with a bucket queue (cell costs <= 8).
func (b *board) loopAround(c castleInfo, code int, wallsOnly bool) loopResult {
	best := loopResult{cost: inf}
	var cost [gn]int
	for i := range cost {
		cost[i] = b.cellCost(i, code, c, wallsOnly)
	}
	dist := make([]int, 2*gn)
	prev := make([]int32, 2*gn)
	var buckets [16][]int32
	for y := c.y - 1; y >= 1; y-- {
		s := y*gw + c.x
		cs := cost[s]
		if cs >= inf || cs >= best.cost {
			continue
		}
		for i := range dist {
			dist[i] = inf
		}
		for i := range buckets {
			buckets[i] = buckets[i][:0]
		}
		dist[2*s] = cs
		prev[2*s] = -1
		buckets[cs&15] = append(buckets[cs&15], int32(2*s))
		pending := 1
		goal := 2*s + 1
		for d := cs; pending > 0 && d-cs < best.cost; d++ {
			bk := &buckets[d&15]
			for len(*bk) > 0 {
				st := int((*bk)[len(*bk)-1])
				*bk = (*bk)[:len(*bk)-1]
				pending--
				if dist[st] != d {
					continue
				}
				if st == goal {
					best.cost = d - cs
					var cells []int
					for v := int(prev[st]); v >= 0 && v != 2*s; v = int(prev[v]) {
						cells = append(cells, v/2)
					}
					best.cells = append(cells, s)
					pending = 0
					break
				}
				i, par := st/2, st%2
				x, yy := i%gw, i/gw
				for dir := 0; dir < 4; dir++ {
					n, nx := i, x
					switch dir {
					case 0:
						n, nx = i-1, x-1
					case 1:
						n, nx = i+1, x+1
					case 2:
						n = i - gw
					case 3:
						n = i + gw
					}
					if n < 0 || n >= gn || nx < 0 || nx >= gw || cost[n] >= inf {
						continue
					}
					np := par
					if dir < 2 && yy < c.y && min(x, nx) == c.x && max(x, nx) == c.x+1 {
						np ^= 1
					}
					ns := 2*n + np
					if nd := d + cost[n]; nd < dist[ns] {
						dist[ns] = nd
						prev[ns] = int32(st)
						buckets[nd&15] = append(buckets[nd&15], int32(ns))
						pending++
					}
				}
			}
		}
	}
	return best
}

// interior marks the cells a loop encloses: everything not reachable from the
// grid edge without crossing loop cells (8-connected, as diagonal gaps leak).
func interior(loop []int) []bool {
	on := make([]bool, gn)
	for _, i := range loop {
		on[i] = true
	}
	seen := make([]bool, gn)
	var st []int
	for i := 0; i < gn; i++ {
		x, y := i%gw, i/gw
		if (x == 0 || y == 0 || x == gw-1 || y == gh-1) && !on[i] {
			seen[i] = true
			st = append(st, i)
		}
	}
	for len(st) > 0 {
		i := st[len(st)-1]
		st = st[:len(st)-1]
		x, y := i%gw, i/gw
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				nx, ny := x+dx, y+dy
				if nx < 0 || ny < 0 || nx >= gw || ny >= gh {
					continue
				}
				n := ny*gw + nx
				if !seen[n] && !on[n] {
					seen[n] = true
					st = append(st, n)
				}
			}
		}
	}
	in := make([]bool, gn)
	for i := range in {
		in[i] = !seen[i] && !on[i]
	}
	return in
}

// ------------------------------------------------------------- plans

var staleMarks int // tests: castles marked enclosed without a closed loop

type plan struct {
	need   []bool // empty cells to build on
	avoid  []bool // inside a kept or planned enclosure: don't waste walls there
	nNeed  int
	secure bool // some castle is enclosed already
}

// planFor works out what player `code` should build next: first one enclosed
// castle (the home castle if it's cheap enough), then the next cheapest castle.
func (b *board) planFor(code int) *plan { return b.planReach(code, 40) }

// planReach: as planFor, expanding only to castles that cost at most `reach` cells.
func (b *board) planReach(code, reach int) *plan {
	p := &plan{need: make([]bool, gn), avoid: make([]bool, gn)}
	type cand struct {
		c    castleInfo
		loop loopResult
	}
	var open []cand
	for _, c := range b.castles {
		if b.land(c.y*gw+c.x) != code {
			continue
		}
		// Enclosed means a closed wall loop around it right now. The grid's
		// enclosure marks (enclosedBy) lag behind: after a battle the castle can
		// still be marked while its ring has holes, and trusting the mark had
		// bots expand elsewhere and never repair it.
		if l := b.loopAround(c, code, true); l.cost == 0 {
			p.secure = true
			for i, v := range interior(l.cells) {
				p.avoid[i] = p.avoid[i] || v
			}
			continue
		} else if b.enclosedBy(c, code) {
			staleMarks++
		}
		l := b.loopAround(c, code, false)
		if l.cost < inf {
			open = append(open, cand{c, l})
		}
	}
	if len(open) == 0 {
		return p
	}
	bi, bs := -1, inf
	for i, o := range open {
		s := o.loop.cost
		if o.c.home && !p.secure {
			s -= 4
		}
		if s < bs {
			bi, bs = i, s
		}
	}
	// A far castle (cost > reach) is still worth building toward when there is
	// nothing else to do: partial walls survive, so the next round starts closer.
	_ = reach
	for _, i := range open[bi].loop.cells {
		if !b.wall(i) && !p.need[i] {
			p.need[i] = true
			p.nNeed++
		}
	}
	for i, v := range interior(open[bi].loop.cells) {
		p.avoid[i] = p.avoid[i] || v
	}
	return p
}

// ------------------------------------------------------------- placements

type placement struct {
	piece, rot, x, y int
	cov              int
	score            float64
}

// placeScore rates putting piece p / rotation r with its 3x3 grid at (x,y).
// ok=false if the piece doesn't fit there.
func (b *board) placeScore(pl *plan, p, r, x, y int) (cov int, score float64, ok bool) {
	extra, inAvoid, touch := 0, 0, 0
	for _, rc := range pieceShapes[p][r] {
		cx, cy := x+rc[1], y+rc[0]
		if cx < 0 || cy < 0 || cx >= gw || cy >= gh {
			return 0, 0, false
		}
		i := cy*gw + cx
		if !b.free(i) {
			return 0, 0, false
		}
		switch {
		case pl.need[i]:
			cov++
		default:
			extra++
			if pl.avoid[i] {
				inAvoid++
			}
			for _, n := range [4]int{i - 1, i + 1, i - gw, i + gw} {
				if n >= 0 && n < gn && (b.wall(n) || pl.need[n]) {
					touch++
					break
				}
			}
		}
	}
	return cov, float64(cov*10-extra*3-inAvoid*8) + 0.5*float64(touch), true
}

// bestPlacement finds the best spot for each of `pieces`. reach (optional)
// gives the cursor's travel distance per cursor cell (-1: unreachable).
func (b *board) bestPlacement(pl *plan, pieces []int, reach []int) placement {
	return b.bestPlacementAt(pl, pieces, reach, nil)
}

// bestPlacementAt: as bestPlacement, trying only cursor cells marked in `at` (nil: all).
func (b *board) bestPlacementAt(pl *plan, pieces []int, reach []int, at []bool) placement {
	best := placement{piece: -1, score: -1e9}
	for _, p := range pieces {
		for r := 0; r < 4; r++ {
			for y := -1; y < gh; y++ {
				for x := -1; x < gw; x++ {
					if at != nil && (x < 0 || y < 0 || !at[y*gw+x]) {
						continue
					}
					if !b.cursorOK(x, y) {
						continue
					}
					d := 0
					if reach != nil {
						if d = reach[y*gw+x]; d < 0 {
							continue
						}
					}
					cov, s, ok := b.placeScore(pl, p, r, x, y)
					if !ok {
						continue
					}
					s -= 0.03 * float64(d)
					s += rand.Float64() * 0.01
					if s > best.score {
						best = placement{p, r, x, y, cov, s}
					}
				}
			}
		}
	}
	return best
}

// cursorBFS returns travel distances (in single moves) over valid cursor cells.
func (b *board) cursorBFS(sx, sy int) []int {
	d := make([]int, gn)
	for i := range d {
		d[i] = -1
	}
	if !b.cursorOK(sx, sy) {
		return d
	}
	d[sy*gw+sx] = 0
	q := []int{sy*gw + sx}
	for len(q) > 0 {
		i := q[0]
		q = q[1:]
		x, y := i%gw, i/gw
		for _, nb := range [4][2]int{{x - 1, y}, {x + 1, y}, {x, y - 1}, {x, y + 1}} {
			if !b.cursorOK(nb[0], nb[1]) {
				continue
			}
			n := nb[1]*gw + nb[0]
			if d[n] < 0 {
				d[n] = d[i] + 1
				q = append(q, n)
			}
		}
	}
	return d
}

// smartPiece picks the piece that best repairs player code's walls (-1: none helps).
func (b *board) smartPiece(code int) int {
	pl := b.planFor(code)
	if pl.nNeed == 0 {
		return -1
	}
	at := make([]bool, gn) // cursor cells whose 3x3 grid touches a needed cell
	for i, v := range pl.need {
		if !v {
			continue
		}
		for dy := 0; dy < 3; dy++ {
			for dx := 0; dx < 3; dx++ {
				if x, y := i%gw-dx, i/gw-dy; x >= 0 && y >= 0 {
					at[y*gw+x] = true
				}
			}
		}
	}
	best, bs := -1, 0.0
	for p := 0; p < 13; p++ {
		pc := b.bestPlacementAt(pl, []int{p}, nil, at)
		if pc.cov > 0 && (best < 0 || pc.score > bs) {
			best, bs = p, pc.score
		}
	}
	return best
}

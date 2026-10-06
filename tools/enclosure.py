"""Exact port of Rampart (DOS) territory detection, RAMPART.EXE 0x184B-0x1AB1.

The grid is 42x27 (40x25 playfield plus a one-cell border), indexed si = y*42 + x.
B[si] = own[si] & 0x30 (wall or already-enclosed).  The algorithm scans in raster
order for a solid cell whose west neighbour is empty and that isn't yet marked,
then walks the contour with a 4-direction pivot rule while counting turns.  A
contour with a negative turn total winds around a hole, so it is walked again
and every east-facing edge cell fills eastwards until a west-edge mark is hit.
"""
W, H = 42, 27
N = W * H                     # 0x46e
MOVE = (-1, -W, 1, W)         # 0 west, 1 north, 2 east, 3 south

def find_enclosed(B):
    """B: list of N ints (non-zero = solid).  Returns set of cell indices the game
    marks enclosed (bit 0x20), including the contour cells it fills from."""
    V = [0] * (N + W + 2)
    filled = set()

    def step(si, d, turns):
        si += MOVE[(d + 1) & 3]
        if B[si] == 0:
            si += MOVE[(d - 1) & 3]
            d = (d + 1) & 3; turns += 1
        else:
            si += MOVE[d]
            if B[si] == 0:
                si += MOVE[d ^ 2]
            else:
                d = (d - 1) & 3; turns -= 1
        if d == 0:
            V[si] = 1
        return si, d, turns

    def fillrun(si):
        start = si
        si += 1
        while True:
            filled.add(si)
            si += 1
            if V[si]:
                filled.add(si)
                break
        filled.add(start)

    si = 0
    while True:
        cl = B[si]
        si += 1
        if si == N:
            break
        if B[si] and not cl and not V[si]:
            V[si] = 1
            start = si
            cur, d, turns = si, 0, 0
            for _ in range(100000):
                cur, d, turns = step(cur, d, turns)
                if cur == start and d == 0:
                    break
            t8 = turns & 0xff
            if t8 & 0x80:                        # negative (signed byte): a hole
                cur, d = start, 0
                for _ in range(100000):
                    cur, d, _t = step(cur, d, 0)
                    if cur == start and d == 0:
                        break
                    if d == 2:
                        fillrun(cur)
    return filled

def grid_from_rows(rows):
    """rows: list of strings using '#' for wall, '.' for empty (40 wide or less)."""
    B = [0] * (N + W + 2)
    for y, r in enumerate(rows):
        for x, c in enumerate(r):
            if c == '#':
                B[(y + 1) * W + (x + 1)] = 0x10
    return B

def render(B, filled, rows=None):
    out = []
    for y in range(H):
        line = ''
        for x in range(W):
            si = y * W + x
            line += '#' if B[si] else ('+' if si in filled else '.')
        out.append(line)
    return '\n'.join(out[: (len(rows) + 2) if rows else H])

if __name__ == '__main__':
    tests = {
        'closed square': ['......', '.####.', '.#..#.', '.#..#.', '.####.', '......'],
        'gap': ['......', '.##.#.', '.#..#.', '.#..#.', '.####.', '......'],
        'diagonal corner': ['......', '.###..', '.#..#.', '.#..#.', '.####.', '......'],
        'diagonal step': ['.......', '.##....', '.#.#...', '.#..#..', '.#...#.', '.#####.'],
        'figure eight': ['.........', '.###.###.', '.#.#.#.#.', '.#######.', '.........'],
    }
    for name, rows in tests.items():
        B = grid_from_rows(rows)
        f = find_enclosed(B)
        print(f'--- {name}: {len([c for c in f if not B[c]])} empty cells enclosed')
        print(render(B, f, rows))

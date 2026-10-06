"""PFT_n: RLE-compressed 40x25 tilemap of u16 tile indices into MBATPF.GRA,
decoded exactly like routine 0x0279 in RAMPART.EXE:
  w & 0x8000 -> (w & 0x7fff) literal tile words follow
  otherwise  -> repeat the next word w times
Runs are row-local: the row ends after 40 tiles."""
import struct
def decode_pft(data, w=40, h=25):
    p = 0; out = []
    def word():
        nonlocal p
        v = struct.unpack_from('<H', data, p)[0]; p += 2; return v
    for y in range(h):
        row = []
        while len(row) < w:
            c = word()
            if c & 0x8000:
                for _ in range(c & 0x7fff):
                    row.append(word())
                    if len(row) == w: break
            else:
                t = word()
                for _ in range(c):
                    row.append(t)
                    if len(row) == w: break
        out.append(row)
    return out, p

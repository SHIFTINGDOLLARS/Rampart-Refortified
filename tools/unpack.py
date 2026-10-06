"""Unpacker for RAMPART.EXE (DOS). Reimplements the LZ decompressor found in the
packer stub at image offset 0xC2BA (stub-relative 0x621..0x6fd)."""
import struct, sys

def load(path):
    d = open(path, 'rb').read()
    h = struct.unpack('<14H', d[:28])
    hdr = h[4] * 16
    size = (h[2] - 1) * 512 + h[1] if h[1] else h[2] * 512
    return d[hdr:size]

def decompress(img, src=0x42):
    s = src
    out = bytearray()
    bp = 0; dl = 0

    def word():
        nonlocal s
        w = img[s] | (img[s + 1] << 8); s += 2; return w

    def byte():
        nonlocal s
        b = img[s]; s += 1; return b

    # initial: lodsw -> bp, dl=16
    bp = word(); dl = 16

    def bit():
        nonlocal bp, dl
        b = bp & 1
        bp >>= 1
        dl -= 1
        if dl == 0:
            bp = word(); dl = 16
        return b

    while True:
        if bit():
            out.append(byte()); continue
        longm = bit()
        bl = byte(); bh = 0xff
        if longm:
            bh = ((bh << 1) | bit()) & 0xff
            if not bit():
                dh = 2
                for _ in range(3):
                    if bit():
                        break
                    bh = ((bh << 1) | bit()) & 0xff
                    dh = (dh << 1) & 0xff
                bh = (bh - dh) & 0xff
            dh = 2
            length = None
            for _ in range(4):
                dh += 1
                if bit():
                    length = dh; break
            if length is None:
                if bit():
                    dh += 1
                    if bit():
                        dh += 1
                    length = dh
                else:
                    if bit():
                        length = byte() + 0x11
                    else:
                        v = 0
                        for _ in range(3):
                            v = (v << 1) | bit()
                        length = v + 9
        else:
            if bit():
                for _ in range(3):
                    bh = ((bh << 1) | bit()) & 0xff
                bh = (bh - 1) & 0xff
                length = 2
            else:
                if bh != bl:
                    length = 2
                else:
                    if not bit():
                        break           # end of stream
                    continue            # segment normalisation marker
        dist = 0x10000 - ((bh << 8) | bl)
        for _ in range(length):
            out.append(out[-dist])
    return bytes(out), s

if __name__ == '__main__':
    img = load(sys.argv[1])
    out, end = decompress(img)
    print(f'decompressed {len(out):#x} bytes, consumed input up to {end:#x}')
    open(sys.argv[2], 'wb').write(out)

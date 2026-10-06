"""Stage 2: undo Microsoft EXEPACK on the image produced by unpack.py and emit a
normal MZ executable (RAMPART_UNPACKED.EXE) plus the flat load image."""
import struct, sys

STAGE1_ENTRY_CS = 0x1498   # far jump target of the outer packer
STAGE1_MINALLOC = 0x7d44

def unexepack(img, cs):
    base = cs * 16
    (ip, rcs, _mem, xsize, sp, ss, dest_len, skip_len, sig) = struct.unpack_from('<8H2s', img, base)
    assert sig == b'RB', sig
    src = bytearray(img[:base - (skip_len - 1) * 16])
    # strip up to 15 bytes of 0xff padding
    end = len(src)
    for _ in range(16):
        if src[end - 1] != 0xff:
            break
        end -= 1
    dst = bytearray(dest_len * 16)
    dst[:len(src)] = src
    s = end; d = len(dst)
    while True:
        cmd = src[s - 1]
        count = src[s - 3] | (src[s - 2] << 8)
        s -= 3
        op = cmd & 0xfe
        if op == 0xb0:
            fill = src[s - 1]; s -= 1
            d -= count
            dst[d:d + count] = bytes([fill]) * count
        elif op == 0xb2:
            s -= count; d -= count
            dst[d:d + count] = src[s:s + count]
        else:
            raise ValueError(f'bad cmd {cmd:#x} at {s:#x}')
        if cmd & 1:
            break
    assert d >= s, (hex(d), hex(s))  # remaining prefix is already in place
    # relocation table
    p = base + 0x12d
    assert img[base + 0x117:base + 0x12d] == b'Packed file is corrupt'
    relocs = []
    for segi in range(16):
        n = img[p] | (img[p + 1] << 8); p += 2
        for _ in range(n):
            off = img[p] | (img[p + 1] << 8); p += 2
            relocs.append((segi * 0x1000, off))
    hdr = dict(ip=ip, cs=rcs, sp=sp, ss=ss)
    return bytes(dst), relocs, hdr

def write_mz(path, image, relocs, hdr, minalloc):
    nrel = len(relocs)
    hsize = 0x1c + nrel * 4
    hpar = (hsize + 15) // 16
    hsize = hpar * 16
    total = hsize + len(image)
    pages = (total + 511) // 512
    last = total % 512
    h = struct.pack('<14H', 0x5a4d, last, pages, nrel, hpar, minalloc, 0xffff,
                    hdr['ss'], hdr['sp'], 0, hdr['ip'], hdr['cs'], 0x1c, 0)
    rel = b''.join(struct.pack('<HH', o, s) for s, o in relocs)
    out = (h + rel).ljust(hsize, b'\0') + image
    open(path, 'wb').write(out)

if __name__ == '__main__':
    img = open(sys.argv[1], 'rb').read()
    image, relocs, hdr = unexepack(img, STAGE1_ENTRY_CS)
    # trim trailing zero bytes into BSS (min alloc) to keep the EXE compact
    trimmed = image.rstrip(b'\0')
    trimmed = trimmed + b'\0' * ((-len(trimmed)) % 16)
    bss_par = (len(image) - len(trimmed)) // 16
    open(sys.argv[2] + '.bin', 'wb').write(image)
    write_mz(sys.argv[2], trimmed, relocs, hdr, bss_par + 0x100)
    print(f'image {len(image):#x} (trimmed {len(trimmed):#x}), {len(relocs)} relocs, '
          f'entry {hdr["cs"]:04x}:{hdr["ip"]:04x} stack {hdr["ss"]:04x}:{hdr["sp"]:04x}')

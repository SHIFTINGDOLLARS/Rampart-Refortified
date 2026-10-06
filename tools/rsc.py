"""RSC archive reader. Layout: u16 count, 18 pad bytes, then `count` entries of
{char name[12]; u32 offset; u32 size}. Offsets are absolute file offsets."""
import struct, sys, os

def read_rsc(path):
    d = open(path, 'rb').read()
    n = struct.unpack_from('<H', d, 0)[0]
    out = []
    for i in range(n):
        p = 0x14 + 20 * i
        name = d[p:p + 12].split(b'\0')[0].decode('latin1')
        off, size = struct.unpack_from('<II', d, p + 12)
        out.append((name, off, size, d[off:off + size]))
    return out

if __name__ == '__main__':
    for path in sys.argv[1:]:
        ents = read_rsc(path)
        print(f'== {os.path.basename(path)} ({len(ents)} entries)')
        for name, off, size, data in ents:
            print(f'  {name:12s} off={off:#08x} size={size:#07x} ({size})  {data[:12].hex()}')

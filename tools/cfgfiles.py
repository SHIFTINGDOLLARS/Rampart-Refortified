"""Decode RAMPART.CFG and RAMPART.HIS.
Both are XORed with a repeating ASCII key (stops at the key's NUL) followed by a
u32 checksum = sum of the plaintext as little-endian u32 words (RAMPART.EXE 0x9056/0x9084)."""
import struct, sys, os

def xor(data, key):
    return bytes(b ^ key[i % len(key)] for i, b in enumerate(data))

def decode(path, key, n):
    raw = open(path, 'rb').read()
    plain = xor(raw[:n], key)
    stored = struct.unpack_from('<I', raw, n)[0] if len(raw) >= n + 4 else None
    calc = sum(struct.unpack_from('<%dI' % (n // 4), plain)) & 0xffffffff
    return plain, stored, calc

KEYS = ['UP', 'DOWN', 'ALT DOWN', 'LEFT', 'RIGHT', 'FIRE', 'ROTATE']

if __name__ == '__main__':
    d = sys.argv[1] if len(sys.argv) > 1 else os.path.join(os.path.dirname(__file__), '..', 'orig', 'rampart')
    cfg, s, c = decode(os.path.join(d, 'RAMPART.CFG'), b'FE_FI_FO_FUM', 0x62)
    print(f'RAMPART.CFG checksum stored={s:#x} computed={c:#x}')
    w = struct.unpack_from('<49H', cfg)
    # DS:0x6c15 = start of file.  Known fields (offsets from file start):
    print('  controller assignment words 0..7:', w[0:8])
    print('  keyboard 1 scancodes (off 0x32):', dict(zip(KEYS, cfg[0x32:0x40:2])))
    print('  keyboard 2 scancodes (off 0x4c):', dict(zip(KEYS, cfg[0x4c:0x5a:2])))
    his, s, c = decode(os.path.join(d, 'RAMPART.HIS'), b'@HI32N4', 0xd8)
    print(f'RAMPART.HIS checksum stored={s:#x} computed={c:#x}')
    for board in range(3):
        rows = []
        for i in range(9):
            o = (board * 9 + i) * 8
            rows.append(f"{his[o:o+3].decode('latin1')} {struct.unpack_from('<I', his, o + 4)[0]}")
        print(f'  board {board}:', ', '.join(rows))

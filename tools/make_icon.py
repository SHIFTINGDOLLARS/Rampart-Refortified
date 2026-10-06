"""Build the app icon (blue player shield from the title screen) and a Go-linkable
Windows resource object (rsrc_windows_amd64.syso) containing it as icon group #1."""
import struct, sys, io, os
import numpy as np
from PIL import Image

ROOT = os.path.join(os.path.dirname(__file__), '..')

def make_icon_images():
    g = np.array(Image.open(os.path.join(ROOT, 'out/assets/screens/GAMETITLE.png')).convert('RGB')).astype(int)
    x0, y0, x1, y1 = 46, 68, 66, 92
    crop = g[y0:y1, x0:x1]
    r, gg, b = crop[..., 0], crop[..., 1], crop[..., 2]
    blue = (b > r + 40) & (b > gg + 40)
    white = (r > 200) & (gg > 200) & (b > 200)
    # keep only the shield (rows 70..89 in screen coords), not the panel below
    rows = np.arange(y0, y1)[:, None] * np.ones(crop.shape[1], int)
    mask = (blue | white) & (rows >= 70) & (rows <= 89)
    # keep the largest 4-connected component (drops bits of the panel below)
    h, w = mask.shape
    lab = np.zeros(mask.shape, int); best = (0, 0)
    for sy in range(h):
        for sx in range(w):
            if mask[sy, sx] and not lab[sy, sx]:
                k = lab.max() + 1; stack = [(sy, sx)]; lab[sy, sx] = k; cnt = 0
                while stack:
                    y, x = stack.pop(); cnt += 1
                    for ny, nx in ((y+1, x), (y-1, x), (y, x+1), (y, x-1)):
                        if 0 <= ny < h and 0 <= nx < w and mask[ny, nx] and not lab[ny, nx]:
                            lab[ny, nx] = k; stack.append((ny, nx))
                if cnt > best[0]: best = (cnt, k)
    mask = lab == best[1]
    out = np.zeros((h, w, 4), np.uint8)
    out[mask, :3] = crop[mask]
    out[mask, 3] = 255
    # 1px dark outline
    pad = np.pad(mask, 1)
    dil = pad[:-2, 1:-1] | pad[2:, 1:-1] | pad[1:-1, :-2] | pad[1:-1, 2:]
    edge = dil & ~mask
    out[edge] = (12, 14, 40, 255)
    ys, xs = np.where(out[..., 3] > 0)
    out = out[ys.min():ys.max() + 1, xs.min():xs.max() + 1]
    src = Image.fromarray(out, 'RGBA')
    imgs = []
    for size in (16, 32, 48, 256):
        scale = max(1, min(size // src.width, size // src.height))
        im = src.resize((src.width * scale, src.height * scale), Image.NEAREST)
        if im.width > size or im.height > size:
            im = src.resize((size * src.width // max(src.size), size * src.height // max(src.size)), Image.LANCZOS)
        canvas = Image.new('RGBA', (size, size), (0, 0, 0, 0))
        canvas.paste(im, ((size - im.width) // 2, (size - im.height) // 2))
        imgs.append(canvas)
    return imgs

def png_bytes(im):
    b = io.BytesIO(); im.save(b, 'PNG'); return b.getvalue()

def write_ico(imgs, path):
    blobs = [png_bytes(i) for i in imgs]
    hdr = struct.pack('<HHH', 0, 1, len(imgs))
    off = 6 + 16 * len(imgs); ents = b''
    for im, bl in zip(imgs, blobs):
        w = im.width if im.width < 256 else 0
        ents += struct.pack('<BBBBHHII', w, w, 0, 0, 1, 32, len(bl), off); off += len(bl)
    open(path, 'wb').write(hdr + ents + b''.join(blobs))
    return blobs

def align(b, n=8):
    return b + b'\0' * ((-len(b)) % n)

MANIFEST = b"""<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security><requestedPrivileges>
      <requestedExecutionLevel level="asInvoker" uiAccess="false"/>
    </requestedPrivileges></security>
  </trustInfo>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
      <supportedOS Id="{1f676c76-80e1-4239-95bb-83d0f6d0da78}"/>
      <supportedOS Id="{4a2f28e3-53b9-4441-ba9c-d69d4a4a6e38}"/>
      <supportedOS Id="{35138b9a-5d96-4fbd-8e2d-a2440225f93a}"/>
    </application>
  </compatibility>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true</dpiAware>
    </windowsSettings>
  </application>
</assembly>
"""

def write_syso(imgs, blobs, path):
    """COFF object with a .rsrc section: RT_ICON ids 1..N, RT_GROUP_ICON 1, RT_MANIFEST 1."""
    n = len(blobs)
    grp = struct.pack('<HHH', 0, 1, n)
    for i, (im, bl) in enumerate(zip(imgs, blobs)):
        w = im.width if im.width < 256 else 0
        grp += struct.pack('<BBBBHHIH', w, w, 0, 0, 1, 32, len(bl), i + 1)
    LANG = 0x0409
    types = [(3, [(i + 1, b) for i, b in enumerate(blobs)]), (14, [(1, grp)]), (24, [(1, MANIFEST)])]
    def dirhdr(nids): return struct.pack('<IIHHHH', 0, 0, 0, 0, 0, nids)
    # layout: root dir, one name dir per type, one lang dir per resource, data entries, data
    o = 16 + 8 * len(types)
    o_type = []
    for t, items in types:
        o_type.append(o); o += 16 + 8 * len(items)
    o_lang = []
    for t, items in types:
        o_lang.append([]);
        for _ in items:
            o_lang[-1].append(o); o += 16 + 8
    nres = sum(len(items) for _, items in types)
    o_ent = o
    o_blobs = o_ent + 16 * nres
    data = b''
    blob_off = []
    for _, items in types:
        for _, b in items:
            data = align(data)
            blob_off.append((o_blobs + len(data), len(b)))
            data += b
    S = 0x80000000
    sec = dirhdr(len(types)) + b''.join(struct.pack('<II', t, S | o_type[k]) for k, (t, _) in enumerate(types))
    for k, (t, items) in enumerate(types):
        sec += dirhdr(len(items)) + b''.join(struct.pack('<II', rid, S | o_lang[k][j]) for j, (rid, _) in enumerate(items))
    e = 0
    for k, (t, items) in enumerate(types):
        for j in range(len(items)):
            sec += dirhdr(1) + struct.pack('<II', LANG, o_ent + 16 * e); e += 1
    relocs = []
    for off, ln in blob_off:
        relocs.append(len(sec))
        sec += struct.pack('<IIII', off, ln, 0, 0)
    assert len(sec) == o_blobs, (len(sec), o_blobs)
    sec += data
    sec = align(sec)

    nsec = 1
    hdr_sz = 20 + 40 * nsec
    raw_ptr = hdr_sz
    reloc_ptr = raw_ptr + len(sec)
    reloc_data = b''.join(struct.pack('<IIH', off, 0, 3) for off in relocs)  # IMAGE_REL_AMD64_ADDR32NB vs symbol 0
    sym_ptr = reloc_ptr + len(reloc_data)
    sym = b'.rsrc\0\0\0' + struct.pack('<IhHBB', 0, 1, 0, 3, 0)
    coff = struct.pack('<HHIIIHH', 0x8664, nsec, 0, sym_ptr, 1, 0, 0)
    sh = b'.rsrc\0\0\0' + struct.pack('<IIIIIIHHI', 0, 0, len(sec), raw_ptr, reloc_ptr, 0, len(relocs), 0, 0xC0000040)
    strtab = struct.pack('<I', 4)
    open(path, 'wb').write(coff + sh + sec + reloc_data + sym + strtab)

if __name__ == '__main__':
    imgs = make_icon_images()
    out_dir = sys.argv[1] if len(sys.argv) > 1 else os.path.join(ROOT, 'player')
    blobs = write_ico(imgs, os.path.join(ROOT, 'out', 'rampart.ico'))
    imgs[-1].save(os.path.join(ROOT, 'out', 'rampart_icon.png'))
    write_syso(imgs, blobs, os.path.join(out_dir, 'rsrc_windows_amd64.syso'))
    print('icon + syso written')

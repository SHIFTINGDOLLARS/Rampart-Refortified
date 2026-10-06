"""Asset extractor for Rampart (DOS). Writes PNG/VOC/XMI files into out/assets."""
import os, struct, sys
import numpy as np
from PIL import Image
sys.path.insert(0, os.path.dirname(__file__))
from rsc import read_rsc

ROOT = os.path.join(os.path.dirname(__file__), '..')
ORIG = os.path.join(ROOT, 'orig', 'rampart')
OUT = os.path.join(ROOT, 'out', 'assets')

def pal(data):
    a = np.frombuffer(data[:768], np.uint8).reshape(256, 3).astype(np.uint16)
    return ((a * 255 + 31) // 63).astype(np.uint8)  # 6-bit VGA -> 8-bit

def img8(pixels, w, h, p, scale=1):
    a = np.frombuffer(pixels[:w * h], np.uint8).reshape(h, w)
    im = Image.fromarray(p[a], 'RGB')
    if scale != 1:
        im = im.resize((w * scale, h * scale), Image.NEAREST)
    return im

def load_gra(name):
    d = open(os.path.join(ORIG, name), 'rb').read()
    return np.frombuffer(d, np.uint8).reshape(-1, 8, 8)

def tilesheet(tiles, p, cols=32):
    n = len(tiles); rows = (n + cols - 1) // cols
    sheet = np.zeros((rows * 9, cols * 9), np.uint8)
    for i, t in enumerate(tiles):
        r, c = divmod(i, cols)
        sheet[r * 9:r * 9 + 8, c * 9:c * 9 + 8] = t
    return Image.fromarray(p[sheet], 'RGB')

def render_tilemap(words, w, h, tiles, p):
    a = np.frombuffer(words, '<u2')[:w * h].reshape(h, w)
    out = np.zeros((h * 8, w * 8), np.uint8)
    for y in range(h):
        for x in range(w):
            t = a[y, x] & 0x7fff
            if t < len(tiles):
                out[y * 8:y * 8 + 8, x * 8:x * 8 + 8] = tiles[t]
    return Image.fromarray(p[out], 'RGB')

def mkdir(*p):
    d = os.path.join(OUT, *p); os.makedirs(d, exist_ok=True); return d

def main():
    title = {n: d for n, _, _, d in read_rsc(os.path.join(ORIG, 'MTITLE.RSC'))}
    trans = {n: d for n, _, _, d in read_rsc(os.path.join(ORIG, 'MTRANS.RSC'))}

    # ---- full-screen 320x200 images
    d = mkdir('screens')
    shots = [('TITLE0', title, 'GAMETITPAL'), ('TITLE1', title, 'TITLE1PAL'),
             ('GAMETITLE', title, 'GAMETITPAL'), ('CONTROLS', title, 'GAMETITPAL'),
             ('GIL_SCREEN', title, 'GIL_PAL'), ('FIN_SCREEN', title, 'FIN_PAL'),
             ('ISLSELECT', trans, None)]
    for name, src, pn in shots:
        p = pal(src[pn]) if pn else pal(read_rsc(os.path.join(ORIG, 'M1PPF.RSC'))[0][3])
        img8(src[name], 320, 200, p).save(os.path.join(d, f'{name}.png'))
    for name, pn in [('GILSCREEN', 'GILPAL'), ('P1DEADSCR', 'P1DEADPAL')]:
        img8(trans[name][10:], 320, 200, pal(trans[pn])).save(os.path.join(d, f'{name}.png'))

    # ---- maps: PFT (RLE tilemap of MBATPF tiles) is what the game draws;
    # PFI is a 320x200 elevation-band image (indices 1-6), PFM/PFC are 40x25 grids.
    from pft import decode_pft
    bat = load_gra('MBATPF.GRA')
    for rsc in ['M1PPF', 'M2PPF', 'M3PPF']:
        ents = {n: dd for n, _, _, dd in read_rsc(os.path.join(ORIG, rsc + '.RSC'))}
        p = pal(ents['PALETTE'])
        dm = mkdir('maps', rsc)
        i = 1
        while f'PFI_{i}' in ents:
            img8(ents[f'PFI_{i}'], 320, 200, p).save(os.path.join(dm, f'PFI_{i}_elevation.png'))
            m, _ = decode_pft(ents[f'PFT_{i}'])
            render_tilemap(np.array(m, '<u2').tobytes(), 40, 25, bat, p).save(os.path.join(dm, f'map_{i}.png'))
            for g in ('PFM', 'PFC'):
                a = np.frombuffer(ents[f'{g}_{i}'], np.uint8).reshape(25, 40)
                open(os.path.join(dm, f'{g}_{i}.txt'), 'w').write(
                    '\n'.join(''.join('%x' % v for v in row) for row in a) + '\n')
            i += 1

    # ---- tilesets with candidate palettes
    m1pal = pal(read_rsc(os.path.join(ORIG, 'M1PPF.RSC'))[0][3])
    dt = mkdir('tilesets')
    for g, pn in [('MBATPF.GRA', None), ('MTETPF.GRA', None), ('MXPLPF.GRA', None),
                  ('MTRNPF.GRA', None), ('MPAUSE.GRA', None),
                  ('MTITPF.GRA', 'GAMETITPAL'), ('MTIT2PF.GRA', 'GAMETITPAL'),
                  ('MHSCPF.GRA', 'HSC_PAL')]:
        p = pal(title[pn]) if pn else m1pal
        tilesheet(load_gra(g), p).save(os.path.join(dt, g.replace('.GRA', '.png')))

    # ---- tilemapped screens (40x25 u16, no header) and boxes (u8 w, u8 h, u16 tiles)
    m1 = {n: dd for n, _, _, dd in read_rsc(os.path.join(ORIG, 'M1PPF.RSC'))}
    gp = pal(m1['PALETTE'])
    ds_ = mkdir('screens', 'tilemapped')
    full = [('HSC0', 'MHSCPF.GRA', 'HSC_PAL'), ('HSC1', 'MHSCPF.GRA', 'HSC_PAL'),
            ('HSC2', 'MHSCPF.GRA', 'HSC_PAL'), ('HSC_MASTERS', 'MHSCPF.GRA', 'HSC_PAL'),
            ('MAINTITLE', 'MTIT2PF.GRA', 'BRICKPAL'), ('CRBRICKBACK', 'MTIT2PF.GRA', 'BRICKPAL'),
            ('BRICKBACK', 'MTITPF.GRA', 'BRICKPAL')]
    for nm, g, pn in full:
        render_tilemap(title[nm], 40, 25, load_gra(g), pal(title[pn])).save(os.path.join(ds_, f'{nm}.png'))
    db = mkdir('boxes')
    trn = load_gra('MTRNPF.GRA')
    for nm, d in trans.items():
        if len(d) >= 2 and len(d) == 2 + d[0] * d[1] * 2:
            render_tilemap(d[2:], d[0], d[1], trn, gp).save(os.path.join(db, f'{nm}.png'))
    for nm in ('TMSGSTART', 'TMSGJOIN', 'TMSGCONT', 'TMSGWAIT', 'TITSDIG'):
        d = title[nm]
        render_tilemap(d[2:], d[0], d[1], load_gra('MTITPF.GRA'), pal(title['GAMETITPAL'])).save(os.path.join(db, f'{nm}.png'))
    dbn = mkdir('banners')
    for nm, d in trans.items():
        if nm.startswith('SW_'):
            img8(d, 320, 40, gp).save(os.path.join(dbn, f'{nm}.png'))

    # ---- audio passthrough
    da = mkdir('sound')
    for n, _, _, dd in read_rsc(os.path.join(ORIG, 'SOUND.RSC')):
        open(os.path.join(da, n + '.voc'), 'wb').write(dd)
    dmu = mkdir('music')
    for n, _, _, dd in read_rsc(os.path.join(ORIG, 'RMUSIC.RSC')):
        if 'XMI' in n:
            open(os.path.join(dmu, n + '.xmi'), 'wb').write(dd)

if __name__ == '__main__':
    main()

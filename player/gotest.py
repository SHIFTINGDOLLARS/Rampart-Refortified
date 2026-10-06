"""Test helpers for the Go headless player."""
import os, subprocess, json, shutil
import numpy as np
from PIL import Image
RH = '/tmp/claude-0/rh'
W = '/tmp/claude-0/gt'
DS = 0x165e
TO_GAME = [(640,'key 39'),(1300,'key 3b 10'),(1450,'key 3b 10')]

def run(name, events, frames, settings=None, savefiles=None, env=None):
    d = f'{W}/{name}'; shutil.rmtree(d, ignore_errors=True); os.makedirs(d)
    sd = f'{d}/save'; os.makedirs(sd)
    if settings: json.dump(settings, open(f'{sd}/settings.json','w'))
    for src in (savefiles or []): shutil.copy(src, sd)
    lines = []
    for f, c in sorted(events, key=lambda e: e[0]):
        c = c.replace('@', d + '/')
        lines.append(f'{f} {c}')
    open(f'{d}/s.txt','w').write('\n'.join(lines)+'\n')
    e = dict(os.environ); e.update(env or {})
    p = subprocess.run([RH, f'{d}/s.txt', str(frames), sd], capture_output=True, text=True, env=e)
    return d, p.stderr

def grids(frame, tag):
    return [(frame, f'dump {DS:x} 35bb 46e @{tag}_t.bin'), (frame, f'dump {DS:x} 3a29 46e @{tag}_o.bin')]

def load_grid(d, tag):
    t = np.fromfile(f'{d}/{tag}_t.bin', np.uint8).reshape(27, 42)
    o = np.fromfile(f'{d}/{tag}_o.bin', np.uint8).reshape(27, 42)
    return t, o

def montage(d, names, out, cols=3):
    ims = [Image.open(f'{d}/{n}') for n in names]
    rows = (len(ims)+cols-1)//cols
    M = Image.new('RGB', (320*cols, 200*rows))
    for i, im in enumerate(ims): M.paste(im, ((i%cols)*320, (i//cols)*200))
    M.save(out)

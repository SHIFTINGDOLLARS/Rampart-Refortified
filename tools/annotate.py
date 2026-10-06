"""Produce an annotated disassembly of RAMPART.EXE code segment 0 from labels.txt.
Usage: python3 annotate.py work/seg0.lin.asm tools/labels.txt > out/rampart_seg0.asm"""
import re, sys

def load_labels(path):
    labels = {}
    for line in open(path):
        line = line.rstrip('\n')
        if not line or line.startswith('#'):
            continue
        m = re.match(r'([0-9A-Fa-f]{4})\s+(\S+)\s*(.*)', line)
        if m:
            labels[int(m.group(1), 16)] = (m.group(2), m.group(3).strip())
    return labels

def main(asm, lab):
    labels = load_labels(lab)
    names = {a: n for a, (n, _) in labels.items()}
    for line in open(asm):
        addr = int(line[:8], 16)
        if addr in labels:
            n, c = labels[addr]
            print(f'\n; ---------------------------------------------------------------')
            print(f'{n}:' + (f'    ; {c}' if c else ''))
        def sub(m):
            t = int(m.group(2), 16)
            return f'{m.group(1)}{names[t]}' if t in names else m.group(0)
        line = re.sub(r'((?:call|jmp|jz|jnz|jc|jnc|ja|jna|jg|jl|jng|jnl|js|jns|loop) (?:short )?)0x([0-9a-f]+)', sub, line.rstrip('\n'))
        print(line)

if __name__ == '__main__':
    main(sys.argv[1], sys.argv[2])

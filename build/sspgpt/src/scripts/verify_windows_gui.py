#!/usr/bin/env python3
"""Fail if any supplied PE executable is not Windows GUI subsystem (2)."""
from pathlib import Path
import struct
import sys

def subsystem(path: Path) -> int:
    data = path.read_bytes()
    if len(data) < 0x40 or data[:2] != b'MZ':
        raise ValueError('not an MZ executable')
    pe = struct.unpack_from('<I', data, 0x3C)[0]
    if pe + 24 + 70 > len(data) or data[pe:pe+4] != b'PE\0\0':
        raise ValueError('invalid PE header')
    optional = pe + 24
    magic = struct.unpack_from('<H', data, optional)[0]
    if magic not in (0x10B, 0x20B):
        raise ValueError(f'unsupported optional-header magic 0x{magic:04x}')
    return struct.unpack_from('<H', data, optional + 68)[0]

bad = False
for arg in sys.argv[1:]:
    p = Path(arg)
    try:
        ss = subsystem(p)
    except Exception as e:
        print(f'PE_VERIFY_FAIL {p}: {e}', file=sys.stderr)
        bad = True
        continue
    print(f'PE_SUBSYSTEM {p.name}={ss}')
    if ss != 2:
        print(f'PE_VERIFY_FAIL {p}: expected Windows GUI subsystem 2, got {ss}', file=sys.stderr)
        bad = True
if bad:
    raise SystemExit(1)

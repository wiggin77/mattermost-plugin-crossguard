#!/usr/bin/env python3
# Emits a 128x128 PNG with a timestamp-derived solid color so each run produces
# a content-unique file. Used by docker-profile-image-test to force
# Mattermost to record a new last_picture_update (re-uploads of byte-identical
# images are de-duplicated by the server and would not advance the timestamp).
import struct
import sys
import time
import zlib

ts = int(time.time() * 1000) & 0xFFFFFF
SIZE = 128
r, g, b = (ts & 0xFF), ((ts >> 8) & 0xFF), ((ts >> 16) & 0xFF)


def chunk(tag: bytes, data: bytes) -> bytes:
    return (
        struct.pack(">I", len(data))
        + tag
        + data
        + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)
    )


sig = b"\x89PNG\r\n\x1a\n"
ihdr = chunk(b"IHDR", struct.pack(">IIBBBBB", SIZE, SIZE, 8, 2, 0, 0, 0))
row = b"\x00" + bytes([r, g, b]) * SIZE
raw = row * SIZE
idat = chunk(b"IDAT", zlib.compress(raw))
iend = chunk(b"IEND", b"")
sys.stdout.buffer.write(sig + ihdr + idat + iend)

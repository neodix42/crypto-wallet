#!/usr/bin/env python3
"""Render CryptoWallet's original vector icon with Python's standard library.

Only the source logo's solid rectangles and polygons are supported. Four-by-four
supersampling keeps rounded edges clean at task-switcher sizes. Committed PNGs
are build inputs; regenerating them requires no runtime or build dependency.
"""
import argparse
import math
from pathlib import Path
import struct
import xml.etree.ElementTree as ET
import zlib

ROOT = Path(__file__).resolve().parents[1]
ASSETS = Path("internal/branding")


def chunk(kind, payload):
    return (struct.pack(">I", len(payload)) + kind + payload
            + struct.pack(">I", zlib.crc32(kind + payload)))


def render(svg, size):
    root = ET.fromstring(svg)
    if root.attrib.get("viewBox") != "0 0 256 256":
        raise ValueError("Expected a 256-unit icon viewBox")
    samples = 4
    dimension = size * samples
    scale = dimension / 256
    pixels = bytearray(dimension * dimension * 4)

    def span(row, left, right, fill):
        first = max(0, math.ceil(left * scale - 0.5))
        last = min(dimension, math.ceil(right * scale - 0.5))
        if first < last:
            start = (row * dimension + first) * 4
            pixels[start:start + (last - first) * 4] = fill * (last - first)

    for shape in root:
        kind = shape.tag.rsplit("}", 1)[-1]
        if kind in ("title", "desc"):
            continue
        color = shape.attrib["fill"]
        if len(color) != 7 or color[0] != "#":
            raise ValueError("Expected an opaque six-digit icon color")
        fill = bytes.fromhex(color[1:]) + b"\xff"
        if kind == "rect":
            x, y, width, height, radius = (float(shape.attrib[key])
                                         for key in ("x", "y", "width", "height", "rx"))
            for row in range(dimension):
                center = (row + 0.5) / scale
                if y <= center < y + height:
                    offset = max(y + radius - center, center - y - height + radius, 0)
                    inset = radius - math.sqrt(max(0, radius * radius - offset * offset))
                    span(row, x + inset, x + width - inset, fill)
        elif kind == "polygon":
            points = [tuple(map(float, point.split(",")))
                      for point in shape.attrib["points"].split()]
            edges = list(zip(points, points[1:] + points[:1]))
            for row in range(dimension):
                center = (row + 0.5) / scale
                crossings = sorted(x1 + (center - y1) * (x2 - x1) / (y2 - y1)
                                   for (x1, y1), (x2, y2) in edges
                                   if min(y1, y2) <= center < max(y1, y2))
                for left, right in zip(crossings[::2], crossings[1::2]):
                    span(row, left, right, fill)
        else:
            raise ValueError("Unsupported icon shape: " + kind)

    raw = bytearray()
    for row in range(size):
        raw.append(0)  # PNG filter None.
        for column in range(size):
            totals = [0, 0, 0, 0]
            for dy in range(samples):
                start = ((row * samples + dy) * dimension + column * samples) * 4
                for dx in range(samples):
                    for channel in range(4):
                        totals[channel] += pixels[start + dx * 4 + channel]
            # Opaque source colors are premultiplied before downsampling so
            # transparent corners do not introduce a dark antialiasing halo.
            alpha = totals[3]
            raw.extend(round(value * 255 / alpha) if alpha else 0 for value in totals[:3])
            raw.append(round(alpha / (samples * samples)))
    header = struct.pack(">IIBBBBB", size, size, 8, 6, 0, 0, 0)
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", header)
            + chunk(b"IDAT", zlib.compress(raw, 9)) + chunk(b"IEND", b""))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="fail if committed PNGs differ from the SVG")
    args = parser.parse_args()
    source = (ROOT / ASSETS / "cryptowallet.svg").read_bytes()
    for name, size in (("cryptowallet.png", 512), ("window-icon.png", 128)):
        path = ROOT / ASSETS / name
        data = render(source, size)
        if args.check:
            if not path.is_file() or path.read_bytes() != data:
                raise SystemExit("FAIL generated icon differs: " + str(path.relative_to(ROOT)))
        else:
            path.write_bytes(data)
    print("PASS original SVG matches 512px desktop and 128px embedded window PNGs")


if __name__ == "__main__":
    main()

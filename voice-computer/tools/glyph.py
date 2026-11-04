"""Draws the voice computer's 12x12 pixel glyphs in the px-tabs styles: g (plain pixels), gw (workbench
dot matrix) and gf (fortress, framed). Usage: python3 tools/glyph.py OUTDIR"""
import os, sys

GLYPHS = {
    "mic": [
        ".....XX.....",
        "....X..X....",
        "....X..X....",
        "....X..X....",
        "....X..X....",
        ".X..X..X..X.",
        ".X...XX...X.",
        "..X......X..",
        "...XXXXXX...",
        ".....XX.....",
        ".....XX.....",
        "...XXXXXX...",
    ],
}

def runs(rows):
    for y, row in enumerate(rows):
        x = 0
        while x < len(row):
            if row[x] == "X":
                s = x
                while x < len(row) and row[x] == "X":
                    x += 1
                yield s, y, x - s
            else:
                x += 1

def dots(rows):
    for y, row in enumerate(rows):
        for x, c in enumerate(row):
            if c == "X":
                yield x, y

def plain(rows):
    r = "".join(f'<rect x="{x}" y="{y}" width="{w}" height="1"/>' for x, y, w in runs(rows))
    return f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="-1 -1 14 14" fill="currentColor" shape-rendering="crispEdges">{r}</svg>\n'

def workbench(rows):
    r = "".join(f'<rect x="{x}.14" y="{y}.14" width="0.72" height="0.72"/>' for x, y in sorted(dots(rows)))
    return f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="-1 -1 14 14" fill="currentColor">{r}</svg>\n'

def fortress(rows):
    frame = ('<rect x="-3" y="-3" width="18" height="1"/><rect x="-3" y="14" width="18" height="1"/>'
             '<rect x="-3" y="-2" width="1" height="16"/><rect x="14" y="-2" width="1" height="16"/>')
    r = "".join(f'<rect x="{x}" y="{y}" width="1" height="1"/>' for x, y in sorted(dots(rows)))
    return f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="-3 -3 18 18" fill="currentColor" shape-rendering="crispEdges">{frame}{r}</svg>\n'

out = sys.argv[1]
for style, f in (("g", plain), ("gw", workbench), ("gf", fortress)):
    os.makedirs(os.path.join(out, style), exist_ok=True)
    for name, rows in GLYPHS.items():
        assert len(rows) == 12 and all(len(r) == 12 for r in rows)
        with open(os.path.join(out, style, name + ".svg"), "w") as fh:
            fh.write(f(rows))

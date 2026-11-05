"""Builds Clingon Mono: JetBrains Mono plus the brand logos in logos/, so
packs can show real logos (`logo: rust`) in clingon's window.

A section's icon gets a two-cell slot in the menu, and everything in it is
centred on that slot, so icons line up whatever they are:

- each logos/NAME.svg (brand logos and icons: see logos/LICENSES.txt) becomes
  a glyph drawn across two cells, scaled by its own outline so all logos are
  the same size. Wordmarks (logos that are just the name, like AWS's or
  Go's) don't read at this size: leave them out.
- each of SYMBOLS gets a twin, the same glyph moved half a cell right, which
  a terminal can't do with the original. (Emoji are two cells already.)

Both live in Unicode's Private Use Area at fixed codepoints: LOGOS and
SYMBOLS only ever grow, so a codepoint never changes meaning.

Separately, each of DRAWN is a character clingon shows that JetBrains Mono
lacks, drawn one cell wide from symbols/NAME.svg at its real codepoint. Without
it the system picks a font for the character, which could be an emoji one.

The font's name carries a hash of what went into it, so a changed font is a
new font to the terminal, never an old one it has cached.

Writes ClingonMono-Regular.otf (embedded in the binary),
internal/config/logos.go (logo names and symbol twins),
internal/window/fontname.go (the font's name) and the list of logos in
pack.schema.json.

Run after adding or changing a logo, from the repo root:

    pip install fonttools skia-pathops
    python3 internal/window/font/build.py
    UPDATE_LOGOS_LOCK=1 go test ./internal/config/   # after adding logos or symbols
"""

import hashlib
import os
import re
import sys

from fontTools.pens.boundsPen import BoundsPen
from fontTools.pens.t2CharStringPen import T2CharStringPen
from fontTools.pens.transformPen import TransformPen
from fontTools.svgLib.path import SVGPath
from fontTools.ttLib import TTFont
from pathops import FillType, Path

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(os.path.dirname(os.path.dirname(HERE)))
SRC = os.path.join(HERE, "JetBrainsMono-Regular.otf")
OUT = os.path.join(HERE, "ClingonMono-Regular.otf")
GO = os.path.join(ROOT, "internal", "config", "logos.go")
GO_NAME = os.path.join(ROOT, "internal", "window", "fontname.go")
SCHEMA = os.path.join(ROOT, "pack.schema.json")

# Append only. A logo dropped later is replaced by None, keeping the rest.
LOGOS = ["apple", "claude", "deno", "docker", "git", "github", "googlecloud", "helm", "java",
         "kubernetes", "node", "ollama", "php", "python", "ruby", "rust", "terraform",
         "clingon",
         "box", "cloud", "cpu", "database", "folder", "globe", "play", "server", "shield", "terminal",
         "amazon", "go",
         "postgres", None, "redis", "mongodb", "sqlite", "cloudflare", "vercel", "cplusplus", None,
         "ansible", "pulumi", "vault", "argo", "nginx", "openai", "gemini", "window", "hash"]
FIRST_LOGO = 0xE000

# Symbols that get a centred twin: the ones packs use, and others that make
# good icons. Append only, as LOGOS. Any not in the font are skipped.
SYMBOLS = ("•▶□▲△≡⇔⊠⊞⇓⇒◎◉∷✓↑✶λ⊛≈∇◔◕◫◶⊕⍟φ⌘⌥◆◇◈○●■▪▫▷▼▽◀◁◊◯◧◨◩◪"
           "⊖⊗⊘⊙⊜⊝⊟⊡∞ΣΩ⌂⚡←→↓↔↕⇐⇑⋄∴∑∆πμψθ∅§✕✗")
FIRST_SYMBOL = 0xE800

# Characters clingon shows that the font lacks, and the symbols/ file drawing
# each. Those files are in font units, with y running down from the top of the
# capitals: 600 wide, and the baseline at 730.
DRAWN = {"☠": "skull", "✎": "pencil"}

# Font units: the cell is 600 wide, capitals 730 tall. Logos are scaled to
# the same height, or width if they're wide, centred on the capitals.
CELL = 600
SLOT = 2 * CELL
HEIGHT, WIDTH = 920, 1100
MIDDLE = 730 // 2


def add(font, glyph, codepoint, draw):
    """Adds a glyph, drawn by draw(pen), one cell wide, at codepoint."""
    cff = font["CFF "].cff
    top = cff.topDictIndex[0]
    pen = T2CharStringPen(CELL, None)
    draw(pen)
    bounds = BoundsPen(None)
    draw(bounds)
    charstrings = top.CharStrings
    charstrings.charStringsIndex.append(pen.getCharString(private=top.Private, globalSubrs=cff.GlobalSubrs))
    charstrings.charStrings[glyph] = len(charstrings.charStringsIndex) - 1
    top.charset.append(glyph)  # the glyph order: main sets it from this
    font["hmtx"][glyph] = (CELL, round(bounds.bounds[0]) if bounds.bounds else 0)
    for t in font["cmap"].tables:
        if t.isUnicode() and t.format in (4, 12):
            t.cmap[codepoint] = glyph


def outline_of(svg, height=0):
    """svg as one outline with y running up, height being its y=0."""
    # Fonts fill by winding, where some SVGs say even-odd: redrawn as one
    # outline, without overlaps, holes stay holes either way.
    outline = Path()
    SVGPath(svg).draw(TransformPen(outline.getPen(), (1, 0, 0, -1, 0, height)))  # SVG's y runs down, the font's up
    with open(svg) as f:
        if 'fill-rule="evenodd"' in f.read():
            outline.fillType = FillType.EVEN_ODD
    outline.simplify(fix_winding=True, keep_starting_points=False)
    return outline


def logo(svg):
    """A pen-drawing function for svg, scaled and centred on the slot."""
    outline = outline_of(svg)
    x0, y0, x1, y1 = outline.bounds
    k = min(HEIGHT / (y1 - y0), WIDTH / (x1 - x0))
    dx = (SLOT - (x1 - x0) * k) / 2 - x0 * k
    dy = MIDDLE - (y0 + y1) / 2 * k
    return lambda pen: outline.draw(TransformPen(pen, (k, 0, 0, k, dx, dy)))


def check(logos, twins):
    """Reads the saved font back: every glyph once, and every codepoint drawing
    the glyph made for it. (Glyphs once came out shifted by one: each
    logo drawn at its neighbour's codepoint.)"""
    font = TTFont(OUT)
    order = font.getGlyphOrder()
    cff = font["CFF "].cff.topDictIndex[0]
    if len(set(order)) != len(order) or len(order) != font["maxp"].numGlyphs or len(order) != len(cff.CharStrings):
        sys.exit("built font is inconsistent: %d glyph names (%d unique), maxp says %d, %d outlines"
                 % (len(order), len(set(order)), font["maxp"].numGlyphs, len(cff.CharStrings)))
    cmap = font.getBestCmap()
    want = {cp: "logo." + n for n, cp in logos.items()}
    want.update({cp: "centred.%04X" % ord(sym) for sym, cp in twins.items()})
    want.update({ord(sym): "drawn." + name for sym, name in DRAWN.items()})
    glyphs = font.getGlyphSet()
    for cp, glyph in want.items():
        if cmap.get(cp) != glyph:
            sys.exit("built font draws %s at U+%04X, not %s" % (cmap.get(cp), cp, glyph))
        bounds = BoundsPen(glyphs)
        glyphs[glyph].draw(bounds)
        if not bounds.bounds:
            sys.exit("built font's %s is empty" % glyph)


def main():
    files = sorted(f[:-4] for f in os.listdir(os.path.join(HERE, "logos")) if f.endswith(".svg"))
    if sorted(n for n in LOGOS if n) != files:
        sys.exit("logos/ and LOGOS differ: add new logos to the end of LOGOS")

    digest = hashlib.sha256()
    for path in ([__file__, SRC] + [os.path.join(HERE, "logos", n + ".svg") for n in files]
                 + [os.path.join(HERE, "symbols", n + ".svg") for n in DRAWN.values()]):
        with open(path, "rb") as f:
            digest.update(f.read())
    tag = digest.hexdigest()[:8]
    family, ps = "Clingon Mono " + tag, "ClingonMono-" + tag

    font = TTFont(SRC)
    logos = {}
    for i, name in enumerate(LOGOS):
        if name:
            add(font, "logo." + name, FIRST_LOGO + i, logo(os.path.join(HERE, "logos", name + ".svg")))
            logos[name] = FIRST_LOGO + i
    glyphs = font.getGlyphSet()
    cmap = font.getBestCmap()
    twins = {}
    for i, sym in enumerate(SYMBOLS):
        if ord(sym) in cmap:
            src = glyphs[cmap[ord(sym)]]
            add(font, "centred.%04X" % ord(sym), FIRST_SYMBOL + i,
                lambda pen, src=src: src.draw(TransformPen(pen, (1, 0, 0, 1, CELL / 2, 0))))
            twins[sym] = FIRST_SYMBOL + i
    for sym, name in DRAWN.items():
        if ord(sym) in cmap:
            sys.exit("the font has %s already: take it out of DRAWN" % sym)
        add(font, "drawn." + name, ord(sym), outline_of(os.path.join(HERE, "symbols", name + ".svg"), 730).draw)
    font.setGlyphOrder(list(font["CFF "].cff.topDictIndex[0].charset))
    font["maxp"].numGlyphs = len(font.getGlyphOrder())

    # A name of its own, so it never clashes with a JetBrains Mono the user
    # has installed (the OFL also asks modified fonts to be renamed).
    cff = font["CFF "].cff
    cff.fontNames[0] = ps
    cff.topDictIndex[0].FullName = family
    cff.topDictIndex[0].FamilyName = family
    for rec in font["name"].names:
        new = {1: family, 3: ps + ";clingon", 4: family, 6: ps, 16: family}.get(rec.nameID)
        if new:
            rec.string = new
    font.save(OUT)
    check(logos, twins)

    def table(f, entries):
        w = max(len(k) for k, _ in entries) + 4  # gofmt's alignment: "key": and a space
        for k, cp in entries:
            f.write('\t%-*s"\\u%04X",\n' % (w, '"%s":' % k, cp))

    with open(GO, "w") as f:
        f.write("// Code generated by internal/window/font/build.py; DO NOT EDIT.\n\n")
        f.write("package config\n\n")
        f.write("// Logos are the brand logos in clingon's font, by the name a pack's\n")
        f.write("// `logo:` uses: each is a glyph drawn across two cells.\n")
        f.write("var Logos = map[string]string{\n")
        table(f, sorted(logos.items()))
        f.write("}\n\n")
        f.write("// Centred maps a symbol to its twin in clingon's font: the same glyph,\n")
        f.write("// centred on two cells like the logos, so icons line up.\n")
        f.write("var Centred = map[string]string{\n")
        table(f, list(twins.items()))
        f.write("}\n")
    with open(GO_NAME, "w") as f:
        f.write("// Code generated by internal/window/font/build.py; DO NOT EDIT.\n\n")
        f.write("package window\n\n")
        f.write("// The font's name changes with its contents, so a terminal never uses\n")
        f.write("// an older one it has cached.\n")
        f.write("const (\n")
        f.write('\tfontFamily   = "%s"\n' % family)
        f.write('\tfontPS       = "%s" // the PostScript name, which Terminal and iTerm match on\n' % ps)
        f.write('\tfontFileName = "%s.otf"\n' % ps)
        f.write(")\n")

    with open(SCHEMA) as f:
        schema = f.read()
    enum = '"logo": {\n      "enum": [\n%s\n      ],' % ",\n".join('        "%s"' % n for n in sorted(logos))
    schema, n = re.subn(r'"logo":\s*\{\s*"enum":\s*\[[^\]]*\],', enum, schema)
    if n != 1:
        sys.exit("pack.schema.json: couldn't find the logo enum")
    with open(SCHEMA, "w") as f:
        f.write(schema)
    print("%s: %d logos, %d centred symbols, %d drawn" % (family, len(logos), len(twins), len(DRAWN)))


if __name__ == "__main__":
    sys.exit(main())

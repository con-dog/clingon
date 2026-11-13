"""Builds Clingon Mono: Intel One Mono plus the brand logos in logos/, so
packs can show real logos (`logo: rust`) in clingon's window.

Intel One Mono is the font, and the only one: no other font's glyphs are in
it. Two weights are built, Regular and Bold, each from Intel One Mono's own,
with the same logos, twins and drawn characters in both, so bold text is the
font's real bold (a terminal's faked one is the regular struck twice, which
smears) and a logo in a bold run is still the logo. It's under the SIL Open Font License (OFL.txt), which asks a changed font
to drop the reserved name "Intel": this one is Clingon Mono.

The cell is tuned to Terminal.app, where clingon's window is 13pt: the
font's ascent and descent (ASCENT, DESCENT) come to a hair under 18pt there
however Terminal rounds them, so a row is 18pt exactly, and the box-drawing
and block characters are stretched to fill that cell, so the logo's two rows
of blocks meet without a seam.

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

Separately, each of DRAWN is a character clingon shows that the font lacks,
drawn one cell wide from symbols/NAME.svg at its real codepoint. Without it
the system picks a font for the character, which could be an emoji one.

The font's name carries a hash of what went into it, so a changed font is a
new font to the terminal, never an old one it has cached.

Writes ClingonMono-Regular.otf and ClingonMono-Bold.otf (embedded in the binary),
internal/config/logos.go (logo names and symbol twins),
and internal/window/fontname.go (the font's name).

Run after adding or changing a logo, from the repo root:

    pip install fonttools skia-pathops
    python3 internal/window/font/build.py
    UPDATE_LOGOS_LOCK=1 go test ./internal/config/   # after adding logos or symbols
"""

import hashlib
import os
import sys

from fontTools.pens.boundsPen import BoundsPen
from fontTools.pens.t2CharStringPen import T2CharStringPen
from fontTools.pens.transformPen import TransformPen
from fontTools.svgLib.path import SVGPath
from fontTools.ttLib import TTFont
from pathops import FillType, Path

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(os.path.dirname(os.path.dirname(HERE)))
# Each weight: Intel One Mono's file, and the one built from it.
WEIGHTS = [("Regular", os.path.join(HERE, "IntelOneMono-Regular.otf"), os.path.join(HERE, "ClingonMono-Regular.otf")),
           ("Bold", os.path.join(HERE, "IntelOneMono-Bold.otf"), os.path.join(HERE, "ClingonMono-Bold.otf"))]
GO = os.path.join(ROOT, "internal", "config", "logos.go")
GO_NAME = os.path.join(ROOT, "internal", "window", "fontname.go")

# Append only. A logo dropped later is replaced by None, keeping the rest.
LOGOS = ["apple", "claude", "deno", "docker", "git", "github", "googlecloud", "helm", "java",
         "kubernetes", "node", "ollama", "php", "python", "ruby", "rust", "terraform",
         "clingon",
         "box", "cloud", "cpu", "database", "folder", "globe", "play", "server", "shield", "terminal",
         "amazon", "go",
         "postgres", None, "redis", "mongodb", "sqlite", "cloudflare", "vercel", "cplusplus", None,
         "ansible", "pulumi", "vault", "argo", "nginx", "openai", "gemini", "window", "hash",
         "hammer", "gear", "puzzle", "checklist",
         "rocket", "wrench", "key", "lock", "book", "bug", "lightning", "chart",
         "mail", "house", "briefcase", "code", "star", "flag"]
FIRST_LOGO = 0xE000

# Symbols that get a centred twin: the ones packs use, and others that make
# good icons. Append only, as LOGOS. Any not in the font are skipped.
SYMBOLS = ("•▶□▲△≡⇔⊠⊞⇓⇒◎◉∷✓↑✶λ⊛≈∇◔◕◫◶⊕⍟φ⌘⌥◆◇◈○●■▪▫▷▼▽◀◁◊◯◧◨◩◪"
           "⊖⊗⊘⊙⊜⊝⊟⊡∞ΣΩ⌂⚡←→↓↔↕⇐⇑⋄∴∑∆πμψθ∅§✕✗")
FIRST_SYMBOL = 0xE800

# Characters clingon shows that the font lacks, and the symbols/ file drawing
# each. Those files are 600 wide, with y running down from the top of
# capitals 730 tall (DRAWN_CELL, DRAWN_CAP): they're scaled to this font's
# capitals and centred on its cell.
DRAWN = {"☠": "skull", "✎": "pencil", "▾": "down", "‖": "bars", "◔": "quarter", "↪": "hook", "❯": "prompt"}

# Drawn although the font has one: its ❯ is a hairline, and this is the
# prompt's mark, the most looked-at character in the window.
REPLACED = "❯"

# Font units: the cell is 614 wide, capitals 655 tall. Logos are scaled to
# HEIGHT, or WIDTH if they're wide, centred on the capitals.
CELL = 614
SLOT = 2 * CELL
CAP = 655
HEIGHT, WIDTH = 920, 1126
MIDDLE = CAP // 2

# The cell's height, from the baseline up and down. At 13pt these are 12.99pt
# and 4.99pt: 18pt whether a terminal rounds each up, rounds each to the
# nearest, or rounds their sum up (a whole 13pt would be 13.00003 in floats,
# and round up to 14).
ASCENT, DESCENT = 999, 384

# Intel One Mono's own cell: what its box-drawing, block and Powerline
# characters are drawn to fill, and so what's stretched to ASCENT and DESCENT.
SRC_CELL = (614, 1020, 360)

# What the symbols/ files are drawn to.
DRAWN_CELL, DRAWN_CAP = 600, 730

# Characters that fill their cell, edge to edge: lines, blocks, Powerline.
CELL_FILLING = [(0x2500, 0x259F), (0xE0A0, 0xE0D7)]


def fills_cell(cp):
    return any(a <= cp <= b for a, b in CELL_FILLING)


def to_cell(cell):
    """The transform taking a character that fills cell (its width, ascent
    and descent) to one that fills this font's."""
    width, ascent, descent = cell
    sx, sy = CELL / width, (ASCENT + DESCENT) / (ascent + descent)
    return (sx, 0, 0, sy, 0, descent * sy - DESCENT)


def drawn():
    """The transform for a symbols/ file's outline: scaled by the capitals'
    heights, and centred on the cell."""
    k = CAP / DRAWN_CAP
    return (k, 0, 0, k, (CELL - DRAWN_CELL * k) / 2, 0)


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
        if t.isUnicode() and t.format in (4, 12) and (t.format == 12 or codepoint <= 0xFFFF):
            t.cmap[codepoint] = glyph


def redraw(font, glyph, transform):
    """Redraws one of the font's own glyphs through transform, in place."""
    cff = font["CFF "].cff
    top = cff.topDictIndex[0]
    src = font.getGlyphSet()[glyph]
    pen = T2CharStringPen(CELL, None)
    src.draw(TransformPen(pen, transform))
    bounds = BoundsPen(None)
    src.draw(TransformPen(bounds, transform))
    top.CharStrings[glyph] = pen.getCharString(private=top.Private, globalSubrs=cff.GlobalSubrs)
    font["hmtx"][glyph] = (CELL, round(bounds.bounds[0]) if bounds.bounds else 0)


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


def check(out, logos, twins):
    """Reads a saved font back: every glyph once, and every codepoint drawing
    the glyph made for it. (Glyphs once came out shifted by one: each
    logo drawn at its neighbour's codepoint.)"""
    font = TTFont(out)
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
    # The cell: what fills it reaches its top and foot, and no wider than it.
    hhea = font["hhea"]
    if (hhea.ascent, hhea.descent, hhea.lineGap) != (ASCENT, -DESCENT, 0):
        sys.exit("built font's cell is %d up, %d down" % (hhea.ascent, hhea.descent))
    bounds = BoundsPen(glyphs)
    glyphs[cmap[0x2588]].draw(bounds)  # █
    if [round(v) for v in bounds.bounds] != [0, -DESCENT, CELL, ASCENT]:
        sys.exit("built font's █ doesn't fill the cell: %s" % (bounds.bounds,))
    if {w for w, _ in font["hmtx"].metrics.values()} - {0, CELL}:
        sys.exit("built font isn't one width")


def build(src, out, weight, family, ps, files):
    """Builds one weight from Intel One Mono's src into out."""
    font = TTFont(src)
    own = font.getBestCmap()

    # The cell, and what's drawn to fill it: each glyph once, though several
    # characters may share one.
    filling = sorted({g for cp, g in own.items() if fills_cell(cp)})
    for glyph in filling:
        redraw(font, glyph, to_cell(SRC_CELL))
    font["hhea"].ascent, font["hhea"].descent, font["hhea"].lineGap = ASCENT, -DESCENT, 0
    os2 = font["OS/2"]
    os2.sTypoAscender, os2.sTypoDescender, os2.sTypoLineGap = ASCENT, -DESCENT, 0
    os2.usWinAscent, os2.usWinDescent = max(os2.usWinAscent, 1080), max(os2.usWinDescent, 444)  # past the cell: lines overshoot it, to join

    logos = {}
    for i, name in enumerate(LOGOS):
        if name:
            add(font, "logo." + name, FIRST_LOGO + i, logo(os.path.join(HERE, "logos", name + ".svg")))
            logos[name] = FIRST_LOGO + i
    glyphs = font.getGlyphSet()
    twins = {}
    for i, sym in enumerate(SYMBOLS):
        cp = ord(sym)
        if cp not in own:
            continue
        add(font, "centred.%04X" % cp, FIRST_SYMBOL + i,
            lambda pen, src=glyphs[own[cp]]: src.draw(TransformPen(pen, (1, 0, 0, 1, CELL / 2, 0))))
        twins[sym] = FIRST_SYMBOL + i
    for sym, name in DRAWN.items():
        if ord(sym) in own and sym not in REPLACED:
            sys.exit("the font has %s already: take it out of DRAWN, or put it in REPLACED" % sym)
        outline = outline_of(os.path.join(HERE, "symbols", name + ".svg"), DRAWN_CAP)
        add(font, "drawn." + name, ord(sym),
            lambda pen, outline=outline: outline.draw(TransformPen(pen, drawn())))
    font.setGlyphOrder(list(font["CFF "].cff.topDictIndex[0].charset))
    font["maxp"].numGlyphs = len(font.getGlyphOrder())

    # A name of its own, so it never clashes with an Intel One Mono the user
    # has installed (its OFL reserves the name "Intel": a changed font can't
    # keep it). Its signature was the unchanged font's.
    if "DSIG" in font:
        del font["DSIG"]
    cff = font["CFF "].cff
    cff.fontNames[0] = ps
    cff.topDictIndex[0].FullName = family
    cff.topDictIndex[0].FamilyName = family
    full, psname = family, ps
    if weight != "Regular":  # the same family, so a terminal finds it as the family's bold
        full, psname = family + " " + weight, ps + "-" + weight
    cff.fontNames[0] = psname
    cff.topDictIndex[0].FullName = full
    for rec in font["name"].names:
        new = {1: family, 2: weight, 3: psname + ";clingon", 4: full, 6: psname, 16: family, 17: weight}.get(rec.nameID)
        if new:
            rec.string = new
    font.save(out)
    return logos, twins, filling


def main():
    files = sorted(f[:-4] for f in os.listdir(os.path.join(HERE, "logos")) if f.endswith(".svg"))
    if sorted(n for n in LOGOS if n) != files:
        sys.exit("logos/ and LOGOS differ: add new logos to the end of LOGOS")

    digest = hashlib.sha256()
    for path in ([__file__] + [src for _, src, _ in WEIGHTS] + [os.path.join(HERE, "logos", n + ".svg") for n in files]
                 + [os.path.join(HERE, "symbols", n + ".svg") for n in DRAWN.values()]):
        with open(path, "rb") as f:
            digest.update(f.read())
    tag = digest.hexdigest()[:8]
    family, ps = "Clingon Mono " + tag, "ClingonMono-" + tag

    for weight, src, out in WEIGHTS:
        logos, twins, filling = build(src, out, weight, family, ps, files)
        check(out, logos, twins)

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
        f.write('\tfontFamily       = "%s"\n' % family)
        f.write('\tfontPS           = "%s" // the PostScript name, which Terminal and iTerm match on\n' % ps)
        f.write('\tfontFileName     = "%s.otf"\n' % ps)
        f.write('\tfontBoldFileName = "%s-Bold.otf" // the bold of the same family, which a terminal uses for bold text\n' % ps)
        f.write(")\n")

    print("%s, %s: %d logos, %d centred symbols, %d drawn, %d fitted to the cell"
          % (family, " and ".join(w for w, _, _ in WEIGHTS), len(logos), len(twins), len(DRAWN), len(filling)))


if __name__ == "__main__":
    sys.exit(main())

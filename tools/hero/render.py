"""Draws clingon's screen as pictures: media/hero.png and media/hero.gif.

Not screenshots of a terminal: each frame is tmux's own record of the screen
(capture-pane -e), drawn here in clingon's font (the one embedded in the
binary) and its window's colours (read from internal/window/window.go), one
cell at a time. So they show clingon as its window does, to the pixel of the
cell grid, without a window, a screen recorder or anyone's desktop in them.

make.sh records the frames and runs this. By hand:

    uv run --with pillow python3 tools/hero/render.py FRAMES_DIR OUT.png OUT.gif

FRAMES_DIR holds NNN.ansi captures and frames.txt, a line per frame:
"NNN.ansi milliseconds cursor_x cursor_y cursor_shown [hero]".
"""

import os
import re
import sys

from PIL import Image, ImageDraw, ImageFont

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
FONT = os.path.join(ROOT, "internal", "window", "font", "ClingonMono-Regular.otf")
FONT_BOLD = os.path.join(ROOT, "internal", "window", "font", "ClingonMono-Bold.otf")

# The window's colours and its 16, from where clingon keeps them.
with open(os.path.join(ROOT, "internal", "window", "window.go")) as f:
    src = f.read()
BG, FG, BOLD, CURSOR, _ = re.search(r'bg, fg, bold, cursor, selection = (.*)', src).group(1).replace('"', "").split(", ")
A16 = re.findall(r'"(#[0-9a-f]{6})"', re.search(r"var ansi = \[16\]string\{(.*?)\n\}", src, re.S).group(1))

# The font's cell (font/build.py): 614 wide, 999 up and 384 down, of 1000.
PT, CELL_W, CELL_H, ASCENT = 13, 8, 18, 0.999


def colour256(n):
    if n < 16:
        return A16[n]
    if n < 232:
        n -= 16
        level = [0, 95, 135, 175, 215, 255]
        return "#%02x%02x%02x" % (level[n // 36], level[n // 6 % 6], level[n % 6])
    v = 8 + (n - 232) * 10
    return "#%02x%02x%02x" % (v, v, v)


def cells(text):
    """A capture as cells: (row, column, character, fg, bg, bold, underlined)."""
    out = []
    for r, line in enumerate(text.rstrip("\n").split("\n")):
        fg, bg, bold, ul, rev = FG, None, False, False, False
        x = 0
        for m in re.finditer(r"\x1b\[([0-9;]*)m|(.)", line):
            if m.group(2) is None:
                p = [int(v) if v else 0 for v in m.group(1).split(";")] if m.group(1) else [0]
                i = 0
                while i < len(p):
                    v = p[i]
                    if v == 0:
                        fg, bg, bold, ul, rev = FG, None, False, False, False
                    elif v == 1:
                        bold = True
                    elif v == 4:
                        ul = True
                    elif v == 7:
                        rev = True
                    elif v == 22:
                        bold = False
                    elif v == 24:
                        ul = False
                    elif v == 27:
                        rev = False
                    elif 30 <= v <= 37:
                        fg = A16[v - 30]
                    elif v == 39:
                        fg = FG
                    elif 40 <= v <= 47:
                        bg = A16[v - 40]
                    elif v == 49:
                        bg = None
                    elif 90 <= v <= 97:
                        fg = A16[v - 82]
                    elif 100 <= v <= 107:
                        bg = A16[v - 92]
                    elif v in (38, 48) and i + 1 < len(p):
                        if p[i + 1] == 5:
                            c = colour256(p[i + 2])
                            i += 2
                        else:
                            c = "#%02x%02x%02x" % tuple(p[i + 2:i + 5])
                            i += 4
                        if v == 38:
                            fg = c
                        else:
                            bg = c
                    i += 1
                continue
            f, b = (fg, bg) if not rev else (bg or BG, fg)
            out.append((r, x, m.group(2), f, b, bold, ul))
            x += 1
    return out


def draw(text, cols, rows, scale, cursor=None):
    """One frame: the screen in a window with a title bar, on black."""
    cw, ch = CELL_W * scale, CELL_H * scale
    pad, bar, edge, radius = 14 * scale, 28 * scale, 6 * scale, 10 * scale
    w, h = cols * cw + 2 * edge, rows * ch + bar + edge
    im = Image.new("RGB", (w + 2 * pad, h + 2 * pad), "#000000")
    d = ImageDraw.Draw(im)
    d.rounded_rectangle([pad, pad, pad + w - 1, pad + h - 1], radius=radius, fill=BG)
    d.rounded_rectangle([pad, pad, pad + w - 1, pad + bar + radius], radius=radius, fill="#2a2636")
    d.rectangle([pad, pad + bar, pad + w - 1, pad + bar + radius], fill=BG)
    for i, c in enumerate(["#ff5f57", "#febc2e", "#28c840"]):
        cx, cy, r = pad + (14 + 20 * i) * scale, pad + bar // 2, 6 * scale
        d.ellipse([cx - r, cy - r, cx + r, cy + r], fill=c)
    font = ImageFont.truetype(FONT, PT * scale)
    bold_font = ImageFont.truetype(FONT_BOLD, PT * scale)
    d.text((pad + w // 2, pad + bar // 2), "clingon", font=font, fill="#b8b4c8", anchor="mm")
    ox, oy = pad + edge, pad + bar
    grid = cells(text)
    for r, x, ch_, f, b, bold, ul in grid:  # backgrounds first: a logo's second half lies over the next cell
        if b and r < rows and x < cols:
            d.rectangle([ox + x * cw, oy + r * ch, ox + x * cw + cw - 1, oy + r * ch + ch - 1], fill=b)
    if cursor:  # the shell's: a block in the window's cursor colour
        d.rectangle([ox + cursor[0] * cw, oy + cursor[1] * ch, ox + cursor[0] * cw + cw - 1, oy + cursor[1] * ch + ch - 1], fill=CURSOR)
    for r, x, ch_, f, b, bold, ul in grid:
        if r >= rows or x >= cols:
            continue
        px, py = ox + x * cw, oy + r * ch
        if ch_ != " ":
            colour = BOLD if bold and f == FG else f
            d.text((px, py + ASCENT * PT * scale), ch_, font=bold_font if bold else font, fill=colour, anchor="ls")
        if ul:
            d.line([px, py + (PT + 3) * scale, px + cw - 1, py + (PT + 3) * scale], fill=f, width=scale)
    return im


def main():
    frames_dir, out_png, out_gif = sys.argv[1:4]
    frames = []
    with open(os.path.join(frames_dir, "frames.txt")) as f:
        for line in f:
            p = line.split()
            if len(p) >= 5:
                frames.append((p[0], int(p[1]), int(p[2]), int(p[3]), p[4] == "1", "hero" in p[5:]))
    if not frames:
        sys.exit("no frames in " + frames_dir)
    texts = [open(os.path.join(frames_dir, f[0])).read() for f in frames]
    plain = [re.sub(r"\x1b\[[0-9;]*m", "", t).rstrip("\n").split("\n") for t in texts]
    cols, rows = max(len(l) for l in plain[0]), len(plain[0])
    pictures = []
    for (name, ms, cx, cy, shown, hero), text in zip(frames, texts):
        cursor = (cx, cy) if shown else None
        if hero:
            draw(text, cols, rows, 2, cursor).save(out_png)
        pictures.append(draw(text, cols, rows, 1, cursor).quantize(colors=128, method=Image.Quantize.MEDIANCUT, dither=Image.Dither.NONE))
    pictures[0].save(out_gif, save_all=True, append_images=pictures[1:], duration=[f[1] for f in frames], loop=0, optimize=True)
    print("%s, %s: %d frames, %dx%d cells" % (out_png, out_gif, len(frames), cols, rows))


if __name__ == "__main__":
    main()

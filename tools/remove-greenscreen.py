#!/usr/bin/env python3
"""Smooth chroma-key removal for solid green-screen logos.

Reads a PNG with a flat green background, feathers the cut edge,
despills residual green, and writes a transparent PNG.
"""

from __future__ import annotations

import argparse
import math
from pathlib import Path

from PIL import Image, ImageFilter


def key_color(img: Image.Image) -> tuple[float, float, float]:
	pixels = img.load()
	w, h = img.size
	samples = [
		pixels[2, 2],
		pixels[w - 3, 2],
		pixels[2, h - 3],
		pixels[w - 3, h - 3],
	]
	r = sum(p[0] for p in samples) / 4
	g = sum(p[1] for p in samples) / 4
	b = sum(p[2] for p in samples) / 4
	return r, g, b


def remove_greenscreen(
	src: Image.Image,
	*,
	hard_dist: float = 55.0,
	soft_dist: float = 95.0,
	blur: float = 1.2,
) -> Image.Image:
	img = src.convert("RGBA")
	pixels = img.load()
	w, h = img.size
	kr, kg, kb = key_color(img)

	alpha = Image.new("L", (w, h))
	ap = alpha.load()
	for y in range(h):
		for x in range(w):
			r, g, b, _ = pixels[x, y]
			dist = math.sqrt((r - kr) ** 2 + (g - kg) ** 2 + (b - kb) ** 2)
			greenness = g - max(r, b)
			if dist < hard_dist and greenness > 40:
				ap[x, y] = 0
			elif dist < soft_dist and greenness > 15:
				t = (dist - hard_dist) / max(soft_dist - hard_dist, 1.0)
				t2 = max(0.0, min(1.0, (greenness - 15) / 40.0))
				ap[x, y] = int(255 * t * (1.0 - 0.65 * t2))
			elif greenness > 70 and g > 180 and r < 120 and b < 120:
				ap[x, y] = 0
			else:
				ap[x, y] = 255

	alpha = alpha.filter(ImageFilter.GaussianBlur(radius=blur))
	ap = alpha.load()

	out = Image.new("RGBA", (w, h))
	op = out.load()
	for y in range(h):
		for x in range(w):
			r, g, b, _ = pixels[x, y]
			a = ap[x, y]
			if a == 0:
				op[x, y] = (0, 0, 0, 0)
				continue
			if a < 250:
				max_rb = max(r, b)
				if g > max_rb:
					g = int(max_rb + (g - max_rb) * (a / 255.0) * 0.35)
			op[x, y] = (r, g, b, a)

	bbox = out.getbbox()
	if not bbox:
		return out
	cropped = out.crop(bbox)
	cw, ch = cropped.size
	side = max(cw, ch)
	pad = int(side * 0.08)
	canvas_side = side + pad * 2
	canvas = Image.new("RGBA", (canvas_side, canvas_side), (0, 0, 0, 0))
	ox = (canvas_side - cw) // 2
	oy = (canvas_side - ch) // 2
	canvas.paste(cropped, (ox, oy), cropped)
	return canvas


def main() -> None:
	parser = argparse.ArgumentParser(description=__doc__)
	parser.add_argument("input", type=Path, help="green-screen PNG")
	parser.add_argument(
		"-o",
		"--output",
		type=Path,
		required=True,
		help="transparent PNG destination",
	)
	parser.add_argument("--hard-dist", type=float, default=55.0)
	parser.add_argument("--soft-dist", type=float, default=95.0)
	parser.add_argument("--blur", type=float, default=1.2)
	args = parser.parse_args()

	result = remove_greenscreen(
		Image.open(args.input),
		hard_dist=args.hard_dist,
		soft_dist=args.soft_dist,
		blur=args.blur,
	)
	args.output.parent.mkdir(parents=True, exist_ok=True)
	result.save(args.output, "PNG")
	print(f"wrote {args.output} ({result.size[0]}x{result.size[1]})")


if __name__ == "__main__":
	main()

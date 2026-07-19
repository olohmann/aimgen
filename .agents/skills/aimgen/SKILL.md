---
name: aimgen
description: >-
  Generate, create, edit, or refine images from a text prompt.
  Use this whenever the user wants to generate, create, render, draw,
  make, or produce an image, picture, photo, illustration, icon, logo, or
  artwork from a description — even if they don't name `aimgen`. Also use it
  to edit, refine, modify, or iterate on an existing image the user provides
  or that aimgen just produced (e.g. "make the sky purple", "add a rainbow",
  inpainting with a mask).
  Do NOT use it to caption/classify/analyze an image, to search the web for
  existing images, or to generate non-image assets (text, audio, video, 3D).
allowed-tools: [Bash]
license: MIT
---

# aimgen — text-to-image via Azure AI Foundry

Drive the `aimgen` CLI to turn a text prompt into image file(s) on disk, or to
edit/refine an existing image. The binary builds the request, calls the Azure AI
Foundry images endpoint, decodes the response, and writes PNG(s). Output
**filenames go to stdout**; status and errors go to stderr.

## When to use

Trigger when the user asks to **generate / create / render / draw / make** an
image, picture, photo, illustration, icon, logo, avatar, or artwork *from a
description*. Look for phrasing like "generate an image of…", "make me a picture
of…", "render a…", "create a logo for…".

Also trigger when the user asks to **edit / refine / modify / adjust / iterate
on** an existing image — either one they provide a path to or one aimgen just
generated. Look for phrasing like "make the sky purple", "add a hat", "change
the background", "now make it darker". Pass the source image with `--image` (see
below); for multi-turn refinement, feed each output back in as the next
`--image`.

Do **not** use when the user wants to: caption, classify, or analyze an existing
image; find/download an existing image from the web; or produce non-image output.

## Error Handling and Initial Configuration

If the CLI cannot be found, or an error occurs about missing Token/Authentication or Configuration,
stop all processing and present the error to the user.

## Command form

```bash
aimgen [flags] "<prompt>"
```

The prompt is a positional argument (or `--prompt "…"`). On success, the written
file path(s) are printed to stdout.

## Key flags

- `-o, --out <path>` — output file stem (default `generated_image.png`). With
  `-n > 1`, files become `<stem>_1.png`, `<stem>_2.png`, …
- `--image <path>` — edit an existing image instead of generating from scratch.
  Repeat `--image` to pass multiple source images (e.g. combine/reference).
  When set, aimgen calls the edits endpoint.
- `--mask <path>` — PNG mask for inpainting; only the masked (transparent)
  region is regenerated. Requires `--image`.
- `-n <int>` — number of images.
- `--size <WxH>` — e.g. `1024x1024`, `1024x1536`, `1536x1024`, must be dividable by 16.
- `--format <png|jpeg|webp>`, `--compression <0-100>`.
- `--model <name>` — defaults to `gpt-image-2`.
- `--quiet` — disable the spinner (use when capturing output non-interactively).
- `--verbose` — print a redacted request/response summary for debugging.

Run `aimgen --help` for the full, authoritative flag list — don't guess.

## Examples

Generate a single image to a named file:
```bash
aimgen -o fox.png "A photograph of a red fox in an autumn forest"
```

Generate three portrait variations:
```bash
aimgen -n 3 --size 1024x1536 -o concept.png "A neon-lit cyberpunk alley in the rain"
```

Edit an existing image:
```bash
aimgen --image fox.png -o fox_sunset.png "change the lighting to a warm sunset"
```

Iteratively refine over multiple turns (feed each output back in as `--image`):
```bash
aimgen --image fox.png       -o step1.png "make the sky purple"
aimgen --image step1.png     -o step2.png "add a rainbow over the trees"
```

Inpaint a region with a mask:
```bash
aimgen --image room.png --mask window_mask.png -o room_edit.png "replace the window with a bookshelf"
```

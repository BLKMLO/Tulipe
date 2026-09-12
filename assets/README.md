# Artwork

`tulipe.png` is the master, 512 pixels square with transparent corners.
`tulipe.ico` is derived from it by `gen_icon.go`, which lives here too:

```bash
go run assets/gen_icon.go                       # rebuild from the master
go run assets/gen_icon.go -in new-artwork.png   # start from something new
```

Turning the `.ico` into the object the Go linker embeds is one more command,
kept separate because it is someone else's tool:

```bash
go run github.com/akavel/rsrc@v0.10.2 -ico assets/tulipe.ico \
    -arch amd64 -o cmd/tulipe/rsrc_windows_amd64.syso
```

Both results are committed, so building Tulipe needs none of this.

## Where the icon shows up: Windows, and only Windows

`cmd/tulipe/rsrc_windows_amd64.syso` is a COFF resource object. `go build`
links any `*_windows_amd64.syso` sitting next to the main package into the
executable, and Explorer, the taskbar and the shortcut dialogue read the icon
from there. Nothing in the Go code refers to it, no other target is affected,
and — the point — **the icon travels inside the one file people download**.

**Linux and macOS get no icon, deliberately.** Both carry it in a file *beside*
the program rather than inside it: a Linux desktop environment reads a
`.desktop` entry and an image installed into the icon theme, and an ELF has no
way to advertise one; the Finder reads an icon from an `.app` bundle, which the
launcher expects to be a windowed application.

Shipping those files alongside the binary is what Tulipe used to do, and it is
what was given up: an archive holds one executable and nothing else, which is
worth more than a menu entry. Embedding them in the binary to write them out on
first run saves nothing — they would still be files on disk, and the project
forbids embedded assets besides.

If a distribution ever packages Tulipe, a `.desktop` entry is five lines and
belongs in that package, not in this archive. It needs `Terminal=true`:
launching a terminal program without one opens a window that closes at once.

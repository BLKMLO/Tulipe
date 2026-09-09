# Artwork

`tulipe.png` is the master, 512 pixels square with transparent corners.
Everything else is derived from it by `gen_icon.go`, which lives here too:

```bash
go run assets/gen_icon.go                       # rebuild from the master
go run assets/gen_icon.go -in new-artwork.png   # start from something new
```

That writes `tulipe.png` and `tulipe.ico`. Turning the `.ico` into the object
the Go linker embeds is one more command, kept separate because it is someone
else's tool:

```bash
go run github.com/akavel/rsrc@v0.10.2 -ico assets/tulipe.ico \
    -arch amd64 -o cmd/tulipe/rsrc_windows_amd64.syso
```

All three results are committed, so building Tulipe needs none of this.

## Where the icon actually shows up

**Windows.** `cmd/tulipe/rsrc_windows_amd64.syso` is a COFF resource object.
`go build` links any `*_windows_amd64.syso` sitting next to the main package
into the executable, and Explorer, the taskbar and the shortcut dialogue read
the icon from there. Nothing in the Go code refers to it, and no other target
is affected.

**Linux.** `tulipe.desktop` and `tulipe.png` are what a desktop environment
needs to show Tulipe in its menu:

```bash
install -Dm644 assets/tulipe.png     ~/.local/share/icons/hicolor/512x512/apps/tulipe.png
install -Dm644 assets/tulipe.desktop ~/.local/share/applications/tulipe.desktop
```

The entry sets `Terminal=true`: Tulipe is a terminal program, and launching it
without one would open a window that closes immediately.

**macOS.** A bare command-line binary cannot carry an icon — the Finder reads
one from an `.app` bundle, which is a directory with a property list and an
`.icns` file, and which the launcher expects to be a windowed application.
Wrapping a terminal program in one means shipping a bundle whose only job is to
open Terminal and run the binary. That is a worse thing to install than the
binary itself, so Tulipe does not ship one, and on macOS the icon is simply the
one the terminal already shows.

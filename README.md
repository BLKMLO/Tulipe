<div align="center">

# 🌷 Tulipe

**Translate an entire book into another language — without breaking it.**

Tulipe takes an EPUB, has it translated chapter by chapter by the AI model of
your choice, and hands you back a book that opens exactly like the original:
same layout, same images, same table of contents. Only the language has
changed.

[Download](https://github.com/BLKMLO/Tulipe/releases/latest) ·
[Getting started](#getting-started) ·
[Supported services](#choosing-a-service)

</div>

---

```
🌷 Tulipe  ·  translation under way

███████████████████████░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░  40%  2/5 documents

  ✓ I. Le retour au pays      47/47 segments in 52s
  ✓ II. La lettre             61/62 segments in 1m11s  ⚑ 1
  ⣾ III. Sous les tilleuls    23/58 segments
  · IV. L'hiver
  · V. Le départ

│  elapsed 3m34s
│  tokens  48 210 in · 19 844 out

esc cancel (work already done is kept)
```

*The interface speaks English by default and French on request — one setting,
independent of the language your books are translated into.*

## Why Tulipe

**Your book comes out intact.** Tulipe never asks the model to rewrite your
files: it finds the passages of prose, has them translated, and puts them back
exactly where they were. Everything else — formatting, images, footnotes,
links — isn't even touched.

**You choose who translates.** Thirteen services, several with a free tier:
Google AI Studio, Mistral, Groq, Cerebras, NVIDIA, Cohere, Cloudflare. Or
Claude, or DeepL. Or a model running on your own machine, in which case your
book never leaves your computer.

**An interruption costs nothing.** Network drop, closed window, quota hit:
just run it again, Tulipe resumes at the next chapter. You never pay twice for
a chapter already translated.

**No silent failure.** If a passage couldn't be translated, it stays in the
original language and Tulipe says so. Once the book is done it sends those
passages back out on its own, in smaller pieces — and if any are still
missing, it offers to try again, without re-paying for the chapter. You won't
discover at chapter 12 that an invalid key handed you a copy of the original.

## Installation

Download the archive for your system from the
[latest release](https://github.com/BLKMLO/Tulipe/releases/latest), extract
the `tulipe` file it contains, and put it wherever you like.

| Your system | File to grab |
|---|---|
| Linux | `tulipe-…-linux-amd64.tar.gz` |
| macOS | `tulipe-…-macos-amd64.tar.gz` |
| Windows | `tulipe-…-windows-amd64.zip` |

A `SHA256SUMS` file ships alongside the archives if you want to verify what
you downloaded:

```bash
sha256sum -c SHA256SUMS --ignore-missing
```

The Linux archive also holds `tulipe.desktop` and `tulipe.png`, if you want
Tulipe in your applications menu:

```bash
install -Dm644 tulipe.png     ~/.local/share/icons/hicolor/512x512/apps/tulipe.png
install -Dm644 tulipe.desktop ~/.local/share/applications/tulipe.desktop
```

Or build from source — all you need is Go 1.24, nothing else:

```bash
go build -o tulipe ./cmd/tulipe
```

## Getting started

**1. Get a key.** From whichever service you choose — `tulipe providers`
lists all thirteen, each with a link to where you get a key and the name of
the variable to store it in. Several don't require a credit card.

```bash
export GROQ_API_KEY=your_key
```

**2. Launch Tulipe.**

```bash
tulipe
```

A menu opens. **Settings** lets you pick the service, the language and the
model — the `m` key asks the service for its list of models. **Test the
connection** checks that everything responds. Then **Translate an EPUB**.

```
🌷 Tulipe  ·  EPUB translation, one chapter at a time

› Translate an EPUB       pick a file and start the translation
  Resume a translation    reuse the chapters already translated
  Settings                model, language, chunking, glossary
  Test the connection     one tiny request to check the model
  Quit

│  model    claude-opus-5 via anthropic
│  language english
│  API key  set (ANTHROPIC_API_KEY)

↑/↓ move  •  enter choose  •  q quit
```

**3. Get your book back.** It shows up next to the original, with the
language in its name: `my-book.fr.epub`. The original is never modified, and
an existing file is never overwritten.

## Interface language

Tulipe's menus, messages and errors are in **English by default**, and in
**French** if you'd rather. It is the first entry in the settings screen:

```
› Interface language      ‹ English ›
  Service                 ‹ anthropic ›
  Model                   claude-opus-5
```

Press `←`/`→` to switch, then `s` to save. On the command line, `--lang fr`
does the same for one run, and the choice is stored as `"language"` in the
configuration file.

This has nothing to do with the language your books are translated into.
Reading a French interface while translating into Japanese is a perfectly
ordinary thing to want, and changing one never touches the other — nor does
it discard the resume cache, since the interface language changes not a word
of the translation.

## Command line

For processing several books, or automating things:

```bash
# the common case
tulipe translate --to French --code fr my-book.epub

# with a free service, and a glossary to keep proper nouns straight
tulipe models --provider groq          # see what models it offers
tulipe translate --provider groq --model <the model you picked> \
                 --to French --code fr \
                 --glossary-file proper-nouns.txt my-book.epub

# plain text instead of EPUB
tulipe translate --to French --code fr --format txt my-book.epub

# on your machine: nothing leaves your computer
tulipe translate --provider ollama --model <your local model> \
                 --to French --code fr my-book.epub
```

The program returns `0` when the book is fully translated, `1` otherwise —
and in that case it still writes the file, naming what's missing.

## Choosing a service

`tulipe providers` shows the full list, and for each one its address, the
expected environment variable, and a link to its sign-up page.

| | Services |
|---|---|
| **Advertises a free tier** | Google AI Studio (Gemini), Mistral, Groq, Cerebras, NVIDIA NIM, Cohere, Cloudflare Workers AI |
| **On your machine** | Ollama, LM Studio |
| **Others** | Claude, OpenAI, OpenRouter, DeepL |

The terms of each free tier live on the service's own site. Tulipe keeps no
copy of them: those limits change too often for a number written here to
still be true by the time you read it.

Same goes for models: Tulipe ships no list. `tulipe models` asks the service
directly, which gives you exact, current names.

```bash
tulipe models --provider groq
```

**DeepL** works a bit differently from the rest. It isn't a model you
instruct, but a translator: you hand it text, it hands back text. In practice
this is more reliable, but the glossary and style notes don't apply to it —
those are instructions, and DeepL doesn't take any.

### Your key stays with you

Tulipe looks for the key in environment variables before checking its
configuration file. A key kept in a variable therefore never touches disk. If
you'd rather store it, the file is created with `0600` permissions and the
key is never displayed or written to a log.

## Two output formats

**EPUB** (default): a real book, identical to the original except for the
language.

**Plain text** (`--format txt`): the prose alone, chapter by chapter, no
markup. Handy for proofreading, comparing two translations, or feeding the
text to another tool.

## Missed passages

A model sometimes stumbles on a paragraph: an empty answer, broken markup, a
nonsensical output. Tulipe then keeps the original text rather than inserting
something dubious, and flags the passage.

Most of the time it is the request that failed, not the paragraph — a batch
came back one string short, or with a sentence of chatter in front of the
answer. So once the book is finished, Tulipe sends the flagged passages back
out by itself: batches of at most four segments instead of forty, and a prompt
that says the batch already came back unusable once. Same rules, same checks,
different shape of request — and usually that is enough.

It waits until the end on purpose. A service having a bad minute is generally
over it twenty chapters later, and you get a complete book to look at either
way. `--salvage=false`, or the **Second attempt** setting, turns it off.

If anything is still missing after that, the end of the run offers to retry:

```
⚑ 3 passage(s) reported — the source text was kept wherever the translation
                          was unusable

p retry the untranslated passages  •  enter back to the menu
```

Only those passages go back to the model — three paragraphs cost three
paragraphs, not three chapters. The offer reappears when you reopen the book,
and in the **Resume a translation** list.

From the command line:

```bash
tulipe translate --retry --to French --code fr my-book.epub
```

## Telling the model what the book is about

One sentence is enough to settle the register and resolve ambiguity — "bar"
doesn't mean the same thing in a noir novel and in a physics textbook.

```bash
tulipe translate --about "a 1950s New York noir novel" \
                 --to French --code fr my-book.epub
```

In the interface, this is the **Book context** field in the settings. Left empty, nothing is added to the prompt.

This sentence is presented to the model as context, never as an instruction:
it cannot override the rules that protect your file.

## Keeping a translation consistent across 300 pages

A book split into hundreds of requests risks drifting: the same character
renamed by chapter 8, an informal tone turning formal partway through. Two
mechanisms prevent that.

The **glossary** pins down vocabulary. One rule per line, restated on every
request:

```
Victory Mansions = Maison de la Victoire
Newspeak = novlangue
```

**Continuity** shows the model the end of the previous passage, purely as
context, so it picks the tone and rhythm back up.

## When things go wrong

A translation is only kept if it holds up. Otherwise the original text is
preserved, and the passage is flagged in the final report.

| What happens | What Tulipe does |
|---|---|
| The model answers off-base | it retries, then splits the batch in two, down to the single paragraph |
| A paragraph stays untranslatable | the original text is kept, retried in smaller pieces at the end of the book, then offered for retry |
| The model returns broken markup | a lone ampersand is repaired; anything else keeps the source |
| The translation contains forbidden characters | the source is kept: such a book wouldn't open |
| Markup comes back broken | the original passage is kept and flagged |
| The service is overloaded | retried, waiting longer each time |
| The service stops responding | the call is abandoned after a timeout, then retried |
| Key refused, quota exhausted | immediate stop, rather than grinding through the book for nothing |
| A chapter yielded nothing | it's marked as failed, never presented as translated |

The output path is checked **before** translation starts: discovering a
missing folder after three hundred pages would be the worst possible moment.
And the produced file is read back before being written — if it wouldn't
open, Tulipe would rather flag the document than hand you a broken book.

The token counts shown are whatever the service reports. When it reports
none, Tulipe says so — it never estimates, and never converts to a currency:
rates change, and a made-up number would be worse than none.

## How your book stays intact

This is the heart of Tulipe, and it comes down to one idea.

An EPUB is a set of XHTML files. The naive approach is to hand a whole
chapter to the model and ask it to return the same file, translated. It
breaks sooner or later: a forgotten tag, a mis-copied entity, a rewritten
attribute — and the reading app refuses the book.

Tulipe never does that. It records **the exact byte position** of every prose
passage in the original file. The model only ever sees those passages. The
translations are then spliced back in at those exact positions, and
everything else in the file is copied over unread: the XML declaration, the
DOCTYPE, stylesheets, images, scripts, attributes.

The translated book is, quite literally, your original book with different
words inside it.

## Contributing

```bash
go test ./...        # the tests
go test -race ./...  # with the race detector
go vet ./...
```

No test calls the network or needs a key: everything runs offline.
[`CLAUDE.md`](CLAUDE.md) describes the architecture and the invariants to
respect, and [`assets/README.md`](assets/README.md) the artwork and where the
icon actually ends up.

To publish a release: **Actions** tab → **Release** → **Run workflow**, enter
the version number (`v0.3.0`). The workflow checks the code, builds the three
binaries, and publishes. Pushing a `v*` tag produces the same result.

## Reference

### What gets translated

Chapter text, the table-of-contents titles, and the title of each document.

Titles are optional. Turn **Translate titles** off in the settings, or pass
`--titles=false`, and chapter headings and the table of contents stay in the
original language while the prose is translated — which is what you want if
the book should still match its reviews and its index.

Not sent to the model: preformatted code blocks, formulas, vector graphics,
and attribute content — so image descriptions. The book's title and the
author's name are also left untouched: translating them is an editorial
decision, not a tool's to make.

A document declaring an encoding other than UTF-8 is left untouched and
flagged, rather than converted at the risk of breaking it.

### Resuming a translation

Translated chapters are kept in your system's cache folder
(`~/.cache/tulipe/` on Linux). Re-running the same book picks up where it
left off; the **Resume a translation** menu entry lists pending jobs, and
`--no-resume` ignores the cache.

Changing the model, language, glossary, context, style notes or the titles
setting starts a fresh translation: the cache accounts for everything that
changes the outcome. Adjusting the batch size doesn't discard it, and neither
does turning the second attempt on or off — that changes how many passages
come back, not what any one of them says.

### Options for `translate`

```
--lang           interface language: en or fr
--to             target language, written the way a human would write it
--code           BCP 47 tag written into the book (fr, es, pt-BR…)
--from           source language (empty: auto-detected)
--from-code      BCP 47 tag of the source (useful for DeepL)
--provider       which service to use (see "tulipe providers")
--model          model identifier (see "tulipe models")
--base-url       base URL of an OpenAI-compatible service
--effort         low, medium, high, xhigh, max (Claude only)
--format         epub (default) or txt
-o               output file
--glossary-file  glossary, one "source = target" rule per line
--style          style notes appended to the instructions
--about          the book in one sentence, to calibrate register
--titles         translate headings and the table of contents (default true)
--salvage        retry the flagged passages once the book is done (default true)
--retry          retry the passages left in the source language
--no-resume      start fresh, without reusing the cache
--quiet          only print the path of the produced file
```

Finer-grained settings, only worth touching if you need to: `--chunk`
(characters per request, 4000), `--max-segments` (40), `--max-tokens`
(16000), `--attempts` (4), `--timeout` (300s), `--context` (400).

### Other commands

```
tulipe                    opens the menu
tulipe book.epub          opens the menu on this book
tulipe providers          known services and the key each expects
tulipe models             the models, asked directly of the service
tulipe config             the current configuration
tulipe version
```

## References

- [EPUB 3.3](https://www.w3.org/TR/epub-33/) — the format specification
- [Open Container Format](https://www.w3.org/TR/epub-33/#sec-ocf) — the archive's structure

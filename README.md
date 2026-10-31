# mdkite

Renders one Markdown file in the browser. No Neovim, no Node, no build step.

```bash
mdkite README.md
```

The file is served on a loopback port and opened in the default browser. Nothing watches the file: reload the tab to see a saved change.

## Features

- GitHub-flavored Markdown: tables, task lists, callouts
- Mermaid diagrams (`.mmd` / `.mermaid` files are wrapped automatically)
- KaTeX math and highlight.js code blocks with copy buttons
- Relative images, YAML front matter panel, dark/light themes

Raw HTML in Markdown is rendered, matching the plugin default. Do not preview untrusted files.

## Cache

Preview assets are downloaded on first use and stored in the user cache directory (`~/.cache/mdkite/cdn` on Linux, honoring `$XDG_CACHE_HOME`). Later runs read from disk; cached assets work offline. Mermaid diagram types and icon packs are lazy-loaded, so a new type needs network once.

Downloads honor `HTTPS_PROXY`. If the process cannot reach a CDN but the browser can, the request falls back to a 307 redirect and is not cached.

```bash
rm -rf ~/.cache/mdkite/cdn   # force a fresh download
```

## Neovim

```lua
vim.keymap.set("n", "<leader>mp", function()
  local name = vim.api.nvim_buf_get_name(0)
  if name == "" then return end
  vim.fn.jobstart({ "mdkite", name }, { detach = true })
end, { desc = "Markdown preview" })
```

The file is read from disk, so save first. Each run is a separate instance and opens a new tab.

## Install

Requires [uv](https://docs.astral.sh/uv/); Python 3.9+ is fetched automatically.

```bash
git clone git@github.com:lenitain/mdkite
cd mdkite
uv tool install .        # puts `mdkite` on PATH (~/.local/bin)
```

Uninstall: `uv tool uninstall mdkite`.

To run from a checkout without installing:

```bash
uv run mdkite README.md
```

The UI lives in `src/mdkite/assets/index.html`, so editing it needs no build step; the next run serves the change.

## Limits

- Saved file only; unsaved buffer contents are not previewed.
- No live reload and no cursor scroll sync: the page renders once per load.
- A CDN asset fetched while offline is not cached; reload with a network.

## License

Python code: MIT, see `LICENSE`.

Preview UI: derived from mdkite.nvim, MIT, Copyright (c) 2025 Selim Acerbas, see `src/mdkite/assets/LICENSE`.

# mdkite

Standalone Go host for the [mdkite.nvim](https://github.com/selimacerbas/mdkite.nvim) browser preview. One binary; no Neovim, Node, or Lua at runtime.

```bash
mdkite README.md
```

The file is served on a loopback port, opened in the default browser, and reloaded when it changes. `mdkite --help` shows the version and the only option.

## Features

- GitHub-flavored Markdown: tables, task lists, callouts
- Mermaid diagrams (`.mmd` / `.mermaid` files are wrapped automatically)
- KaTeX math and highlight.js code blocks with copy buttons
- Relative images, YAML front matter panel, dark/light themes

Raw HTML in Markdown is rendered, matching the plugin default. Do not preview untrusted files.

## Cache

Preview assets are downloaded on first use and stored in the user cache directory (`~/.cache/mdkite/cdn` on Linux, honoring `$XDG_CACHE_HOME`). Later runs read from disk; cached assets work offline. Mermaid diagram types and icon packs are lazy-loaded, so a new type needs network once.

Downloads use Go's proxy environment (`HTTPS_PROXY`). If Go cannot reach a CDN but the browser can, the request falls back to a 307 redirect and is not cached.

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

## Build

Requires Go 1.22+.

```bash
make build      # static ./mdkite
make install    # ~/.local/bin/mdkite
```

## Limits

- Saved file only; unsaved buffer contents are not previewed.
- No cursor scroll sync; there is no editor to provide a cursor.

## License

Go code: MIT, see `LICENSE`.

Preview UI: mdkite.nvim, MIT, Copyright (c) 2025 Selim Acerbas, see `assets/LICENSE`.

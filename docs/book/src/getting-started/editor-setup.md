# Editor setup

Yoru ships syntax highlighting for Neovim. Other editors are easy to support
because the keyword list is small (see [Keyword reference](../appendix/keywords.md)).

## Neovim

The repository ships a vim-regex syntax file plus filetype detection in
`editor/neovim/`. Drop the three files into your Neovim runtime path:

```sh
mkdir -p ~/.config/nvim/{ftdetect,syntax,ftplugin}
cp editor/neovim/ftdetect/yoru.vim ~/.config/nvim/ftdetect/
cp editor/neovim/syntax/yoru.vim   ~/.config/nvim/syntax/
cp editor/neovim/ftplugin/yoru.lua ~/.config/nvim/ftplugin/
```

Open any `*.yr` file and you should see keywords, types, strings, numbers,
operators, and capabilities highlighted.

### What you get

- `*.yr` files auto-detect as the `yoru` filetype.
- All reserved keywords highlighted.
- Reference-capability annotations (`iso`, `trn`, `ref`, `val`, `box`, `tag`)
  highlighted as types.
- Yoru-flavoured operators (`<-`, `|>`, `?`, `??`, `=>`, `->`, `...`)
  highlighted.
- Built-in types (`Int`, `Float`, `String`, `Bool`, `Result`, `Option`,
  `Stream`, etc.) and standard-library namespaces (`HTTP`, `JSON`, `DB`,
  `LLM`, `Log`, ...) highlighted.
- 2-space indent, `//` line comments, 100-column textwidth.

### Tree-sitter (optional, advanced)

The Neovim config in this user's setup uses `nvim-treesitter`. A
production-grade Yoru highlight via tree-sitter requires:

1. A grammar repository (`tree-sitter-yoru`) written in JavaScript.
2. Generated C code compiled to a `.so` parser and dropped in
   `~/.local/share/nvim/site/parser/yoru.so`.
3. `queries/yoru/highlights.scm` written against that grammar.

This is roughly two to three days of work for a thorough grammar. The
shipped vim-regex syntax is correct, fast, and works for every Yoru
construct documented in this book — start there. Upgrade to tree-sitter
only if you need semantic features like incremental folding or
language-aware text objects.

## Other editors

There is no first-party plugin yet. For now:

- **VS Code**: any `.tmLanguage.json` derived from the Neovim syntax file
  works. Patches welcome.
- **JetBrains**: file-type → custom; paste in the keyword set from
  [Appendix](../appendix/keywords.md).
- **Helix**: tree-sitter required (see above).
- **Emacs**: `define-derived-mode` from `prog-mode` with the keyword list
  from the appendix.

-- Yoru filetype settings.
-- Loaded automatically when a buffer's filetype is set to `yoru`.

vim.bo.commentstring = "// %s"
vim.bo.comments      = "s1:/*,mb:*,ex:*/,://"
vim.bo.expandtab     = true
vim.bo.shiftwidth    = 2
vim.bo.softtabstop   = 2
vim.bo.tabstop       = 2
vim.bo.autoindent    = true
vim.bo.smartindent   = true

-- Yoru tool/agent declarations frequently put long descriptions on one
-- line. The default synmaxcol (often 200 in performance-tuned configs)
-- cuts syntax processing mid-string, which would let a long string
-- bleed past EOL into the next lines. Combined with `oneline` on the
-- string region, this keeps both highlighting AND boundaries correct.
vim.opt_local.synmaxcol = 3000

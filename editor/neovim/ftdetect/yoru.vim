" Yoru filetype detection.
" Maps *.yr files (and the common .yoru extension) to the `yoru` filetype.

autocmd BufRead,BufNewFile *.yr   set filetype=yoru
autocmd BufRead,BufNewFile *.yoru set filetype=yoru

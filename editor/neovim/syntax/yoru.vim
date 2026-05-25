" Vim syntax file
" Language: Yoru
" Maintainer: Yoru contributors
" Filenames: *.yr, *.yoru
"
" Drop this file at ~/.config/nvim/syntax/yoru.vim and a matching
" ftdetect/yoru.vim in ~/.config/nvim/ftdetect/.

if exists("b:current_syntax")
  finish
endif

syntax case match

syntax keyword yoruTodo TODO FIXME XXX NOTE HACK contained

syntax match  yoruEscape      "\\[nrt\\\"0]" contained
" `oneline` guards against the string region bleeding past EOL when Vim's
" synmaxcol cap (default 3000, but some configs set it lower) truncates
" syntax processing before the closing quote is seen. Without oneline,
" a long string would never close and would paint every following line as
" yoruString. Yoru strings are single-line by design, so this matches the
" language too.
syntax region yoruString      start=+"+ skip=+\\"+ end=+"+ oneline contains=yoruEscape

syntax match yoruFloat  "\v<\d+\.\d+([eE][+-]?\d+)?>"
syntax match yoruFloat  "\v<\d+[eE][+-]?\d+>"
syntax match yoruInt    "\v<\d+>"
syntax match yoruHex    "\v<0x[0-9a-fA-F]+>"

syntax keyword yoruStructure object blueprint actor agent tool mcp service pipeline protocol impl effect handle flow delegate
syntax keyword yoruDeclaration fn let mut type enum union
syntax keyword yoruConcurrency spawn receive send emit yield
syntax keyword yoruConditional if else match
syntax keyword yoruRepeat      for in while do
syntax keyword yoruStatement   return break continue
syntax keyword yoruInclude import export use where with
syntax keyword yoruSelf self super
syntax keyword yoruReserved async await
syntax keyword yoruPipelineStage stream partition merge window sink source transform
syntax keyword yoruBoolean true false
syntax keyword yoruNil     nil
syntax keyword yoruCapability iso trn ref val box tag

syntax keyword yoruType Int Float Bool String Bytes Void Self
syntax keyword yoruType Result Ok Err Option Some None
syntax keyword yoruType Stream Map List Object Request Response
syntax keyword yoruType Order User Permission Role Resource

" Lookahead `(.)@=` only highlights the name when it's used as a namespace
" prefix, not as a bare type constructor.
syntax match yoruNamespace "\v<(HTTP|JSON|DB|LLM|Log|Crypto|Time|Bytes|Map|List|Collector|Redis|Rabbit|SQS|Kafka|Supervisor|Child|CORS|JWT|RateLimit|RequestLog|HTTPProvider|Resource|Anthropic|MockHTTP|Postgres|MySQL|SQLite|HL7Parser|FHIRMapper|FHIRStore|Kafka)>(\.)@="

syntax match yoruAnnotation "\v\@\w+"

syntax region yoruEffectList matchgroup=yoruDeclaration start="\<effect\s*\[" end="\]" contains=yoruEffectName,yoruDelimiter
syntax match  yoruEffectName "\v<\u\w*>" contained

syntax match yoruOperator "->"
syntax match yoruOperator "=>"
syntax match yoruOperator "|>"
syntax match yoruOperator "<-"
syntax match yoruOperator "??"
syntax match yoruOperator "?"
syntax match yoruOperator "\.\.\."
syntax match yoruOperator "+="
syntax match yoruOperator "-="
syntax match yoruOperator "=="
syntax match yoruOperator "!="
syntax match yoruOperator "<="
syntax match yoruOperator ">="
syntax match yoruOperator "&&"
syntax match yoruOperator "||"
" Single-char operators. `/` and `*` are intentionally omitted: they would
" otherwise shadow the `//` and `/*` comment delimiters via Vim's
" single-char-match priority.
syntax match yoruOperator "[+\-%=<>!]"

syntax match yoruDelimiter "[(){}\[\];,:.]"

" Match `fn <name>` so the name highlights as a function definition.
syntax match yoruFunctionName "\v(\<fn\s+)@<=\h\w*"

" Capitalised identifier followed by `{` — treat as a type constructor call.
syntax match yoruTypeCtor "\v<\u\w*>(\s*\{)@="

" Comments defined last so the region START patterns win Vim's tie-break
" against earlier match items. The explicit `contains=` whitelist stops
" keywords/types/operators from rendering inside comment text.
syntax region yoruLineComment  start="//" end="$" oneline keepend contains=yoruTodo,@Spell
syntax region yoruBlockComment start="/\*" end="\*/" keepend contains=yoruTodo,yoruBlockComment,@Spell

highlight default link yoruTodo            Todo
highlight default link yoruLineComment     Comment
highlight default link yoruBlockComment    Comment
highlight default link yoruString          String
highlight default link yoruEscape          SpecialChar
highlight default link yoruInt             Number
highlight default link yoruFloat           Float
highlight default link yoruHex             Number

highlight default link yoruStructure       Structure
highlight default link yoruDeclaration     Keyword
highlight default link yoruConcurrency     Keyword
highlight default link yoruConditional     Conditional
highlight default link yoruRepeat          Repeat
highlight default link yoruStatement       Statement
highlight default link yoruInclude         Include
highlight default link yoruSelf            Identifier
highlight default link yoruReserved        Keyword
highlight default link yoruPipelineStage   Function

highlight default link yoruBoolean         Boolean
highlight default link yoruNil             Constant

highlight default link yoruCapability      StorageClass
highlight default link yoruType            Type
highlight default link yoruNamespace       Type
highlight default link yoruEffectName      Type

highlight default link yoruAnnotation      PreProc
highlight default link yoruOperator        Operator
highlight default link yoruDelimiter       Delimiter

highlight default link yoruFunctionName    Function
highlight default link yoruTypeCtor        Type

let b:current_syntax = "yoru"

package lexer

// TokenType is a string for readable test output.
type TokenType string

const (
	// Special
	ILLEGAL TokenType = "ILLEGAL"
	EOF     TokenType = "EOF"
	NEWLINE TokenType = "NEWLINE"

	// Identifiers and literals
	IDENT        TokenType = "IDENT"
	INT_LIT      TokenType = "INT_LIT"
	FLOAT_LIT    TokenType = "FLOAT_LIT"
	STRING_LIT   TokenType = "STRING_LIT"
	DURATION_LIT TokenType = "DURATION_LIT" // e.g. 60s, 500ms, 1.5h

	// Keywords — structural
	OBJECT    TokenType = "OBJECT"
	BLUEPRINT TokenType = "BLUEPRINT"
	ACTOR     TokenType = "ACTOR"
	AGENT     TokenType = "AGENT"
	TOOL      TokenType = "TOOL"
	MCP       TokenType = "MCP"
	SERVICE   TokenType = "SERVICE"
	PIPELINE  TokenType = "PIPELINE"
	PROTOCOL  TokenType = "PROTOCOL"
	IMPL      TokenType = "IMPL"
	EFFECT    TokenType = "EFFECT"
	HANDLE    TokenType = "HANDLE"
	FLOW      TokenType = "FLOW"

	// Keywords — functions and bindings
	FN     TokenType = "FN"
	LET    TokenType = "LET"
	MUT    TokenType = "MUT"
	TYPE   TokenType = "TYPE"
	ENUM   TokenType = "ENUM"
	UNION  TokenType = "UNION"

	// Keywords — concurrency
	SPAWN   TokenType = "SPAWN"
	RECEIVE TokenType = "RECEIVE"
	SEND    TokenType = "SEND"
	EMIT    TokenType = "EMIT"
	YIELD   TokenType = "YIELD"

	// Keywords — control flow
	MATCH    TokenType = "MATCH"
	IF       TokenType = "IF"
	ELSE     TokenType = "ELSE"
	FOR      TokenType = "FOR"
	IN       TokenType = "IN"
	WHILE    TokenType = "WHILE"
	DO       TokenType = "DO"
	RETURN   TokenType = "RETURN"
	BREAK    TokenType = "BREAK"
	CONTINUE TokenType = "CONTINUE"

	// Keywords — modules
	IMPORT TokenType = "IMPORT"
	EXPORT TokenType = "EXPORT"
	USE    TokenType = "USE"
	WHERE  TokenType = "WHERE"
	WITH   TokenType = "WITH"

	// Keywords — literals
	TRUE  TokenType = "TRUE"
	FALSE TokenType = "FALSE"
	NIL   TokenType = "NIL"

	// Keywords — self/super
	SELF  TokenType = "SELF"
	SUPER TokenType = "SUPER"

	// Keywords — reference capabilities
	ISO TokenType = "ISO"
	TRN TokenType = "TRN"
	REF TokenType = "REF"
	VAL TokenType = "VAL"
	BOX TokenType = "BOX"
	TAG TokenType = "TAG"

	// Keywords — pipeline
	STREAM    TokenType = "STREAM"
	PARTITION TokenType = "PARTITION"
	MERGE     TokenType = "MERGE"
	WINDOW    TokenType = "WINDOW"
	SINK      TokenType = "SINK"
	SOURCE    TokenType = "SOURCE"
	TRANSFORM TokenType = "TRANSFORM"

	// Keywords — composition
	DELEGATE TokenType = "DELEGATE"

	// Keywords — reserved for future
	ASYNC TokenType = "ASYNC"
	AWAIT TokenType = "AWAIT"

	// Operators
	ARROW           TokenType = "ARROW"           // ->
	FAT_ARROW       TokenType = "FAT_ARROW"       // =>
	PIPE_FORWARD    TokenType = "PIPE_FORWARD"     // |>
	SEND_OP         TokenType = "SEND_OP"          // <-
	QUESTION        TokenType = "QUESTION"         // ?
	DOUBLE_QUESTION TokenType = "DOUBLE_QUESTION"  // ??
	SPREAD          TokenType = "SPREAD"           // ...

	// Arithmetic
	PLUS    TokenType = "PLUS"
	MINUS   TokenType = "MINUS"
	STAR    TokenType = "STAR"
	SLASH   TokenType = "SLASH"
	PERCENT TokenType = "PERCENT"

	// Comparison
	EQ  TokenType = "EQ"
	NEQ TokenType = "NEQ"
	LT  TokenType = "LT"
	GT  TokenType = "GT"
	LTE TokenType = "LTE"
	GTE TokenType = "GTE"

	// Logical
	AND  TokenType = "AND"
	OR   TokenType = "OR"
	BANG TokenType = "BANG"

	// Assignment
	ASSIGN       TokenType = "ASSIGN"       // =
	PLUS_ASSIGN  TokenType = "PLUS_ASSIGN"  // +=
	MINUS_ASSIGN TokenType = "MINUS_ASSIGN" // -=

	// Delimiters
	LPAREN   TokenType = "LPAREN"
	RPAREN   TokenType = "RPAREN"
	LBRACE   TokenType = "LBRACE"
	RBRACE   TokenType = "RBRACE"
	LBRACKET TokenType = "LBRACKET"
	RBRACKET TokenType = "RBRACKET"
	COMMA    TokenType = "COMMA"
	COLON    TokenType = "COLON"
	SEMICOLON TokenType = "SEMICOLON"
	DOT      TokenType = "DOT"

	// Annotation
	AT TokenType = "AT"
)

// Token represents a lexed token with position info.
type Token struct {
	Type    TokenType
	Literal string
	Line    int
	Col     int
}

var keywords = map[string]TokenType{
	"object":    OBJECT,
	"blueprint": BLUEPRINT,
	"actor":     ACTOR,
	"agent":     AGENT,
	"tool":      TOOL,
	"mcp":       MCP,
	"service":   SERVICE,
	"pipeline":  PIPELINE,
	"protocol":  PROTOCOL,
	"impl":      IMPL,
	"effect":    EFFECT,
	"handle":    HANDLE,
	"flow":      FLOW,
	"fn":        FN,
	"let":       LET,
	"mut":       MUT,
	"type":      TYPE,
	"enum":      ENUM,
	"union":     UNION,
	"spawn":     SPAWN,
	"receive":   RECEIVE,
	"send":      SEND,
	"emit":      EMIT,
	"yield":     YIELD,
	"match":     MATCH,
	"if":        IF,
	"else":      ELSE,
	"for":       FOR,
	"in":        IN,
	"while":     WHILE,
	"do":        DO,
	"return":    RETURN,
	"break":     BREAK,
	"continue":  CONTINUE,
	"import":    IMPORT,
	"export":    EXPORT,
	"use":       USE,
	"where":     WHERE,
	"with":      WITH,
	"true":      TRUE,
	"false":     FALSE,
	"nil":       NIL,
	"self":      SELF,
	"super":     SUPER,
	"iso":       ISO,
	"trn":       TRN,
	"ref":       REF,
	"val":       VAL,
	"box":       BOX,
	"tag":       TAG,
	"stream":    STREAM,
	"partition": PARTITION,
	"merge":     MERGE,
	"window":    WINDOW,
	"sink":      SINK,
	"source":    SOURCE,
	"transform": TRANSFORM,
	"delegate":  DELEGATE,
	"async":     ASYNC,
	"await":     AWAIT,
}

// LookupIdent returns the keyword token type for ident, or IDENT if it isn't a keyword.
func LookupIdent(ident string) TokenType {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return IDENT
}

package lexer

// Lexer tokenizes Yoru source. line/col are 1-based to match editor reports.
type Lexer struct {
	input   string
	pos     int
	readPos int // one ahead of pos; peekChar reads from here
	ch      byte
	line    int
	col     int
}

func New(input string) *Lexer {
	l := &Lexer{input: input, line: 1, col: 0}
	l.readChar()
	return l
}

func (l *Lexer) readChar() {
	if l.readPos >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = l.input[l.readPos]
	}
	l.pos = l.readPos
	l.readPos++
	l.col++
}

func (l *Lexer) peekChar() byte {
	if l.readPos >= len(l.input) {
		return 0
	}
	return l.input[l.readPos]
}

func (l *Lexer) NextToken() Token {
	l.skipWhitespace()

	startLine := l.line
	startCol := l.col

	var tok Token

	switch l.ch {
	case 0:
		tok = Token{Type: EOF, Literal: "", Line: startLine, Col: startCol}
	case '\n':
		tok = Token{Type: NEWLINE, Literal: "\n", Line: startLine, Col: startCol}
		l.line++
		l.col = 0
		l.readChar()
		return tok
	case '-':
		if l.peekChar() == '>' {
			l.readChar()
			tok = Token{Type: ARROW, Literal: "->", Line: startLine, Col: startCol}
		} else if l.peekChar() == '=' {
			l.readChar()
			tok = Token{Type: MINUS_ASSIGN, Literal: "-=", Line: startLine, Col: startCol}
		} else {
			tok = Token{Type: MINUS, Literal: "-", Line: startLine, Col: startCol}
		}
	case '=':
		if l.peekChar() == '>' {
			l.readChar()
			tok = Token{Type: FAT_ARROW, Literal: "=>", Line: startLine, Col: startCol}
		} else if l.peekChar() == '=' {
			l.readChar()
			tok = Token{Type: EQ, Literal: "==", Line: startLine, Col: startCol}
		} else {
			tok = Token{Type: ASSIGN, Literal: "=", Line: startLine, Col: startCol}
		}
	case '|':
		if l.peekChar() == '>' {
			l.readChar()
			tok = Token{Type: PIPE_FORWARD, Literal: "|>", Line: startLine, Col: startCol}
		} else if l.peekChar() == '|' {
			l.readChar()
			tok = Token{Type: OR, Literal: "||", Line: startLine, Col: startCol}
		} else {
			tok = Token{Type: ILLEGAL, Literal: string(l.ch), Line: startLine, Col: startCol}
		}
	case '<':
		if l.peekChar() == '-' {
			l.readChar()
			tok = Token{Type: SEND_OP, Literal: "<-", Line: startLine, Col: startCol}
		} else if l.peekChar() == '=' {
			l.readChar()
			tok = Token{Type: LTE, Literal: "<=", Line: startLine, Col: startCol}
		} else {
			tok = Token{Type: LT, Literal: "<", Line: startLine, Col: startCol}
		}
	case '>':
		if l.peekChar() == '=' {
			l.readChar()
			tok = Token{Type: GTE, Literal: ">=", Line: startLine, Col: startCol}
		} else {
			tok = Token{Type: GT, Literal: ">", Line: startLine, Col: startCol}
		}
	case '?':
		if l.peekChar() == '?' {
			l.readChar()
			tok = Token{Type: DOUBLE_QUESTION, Literal: "??", Line: startLine, Col: startCol}
		} else {
			tok = Token{Type: QUESTION, Literal: "?", Line: startLine, Col: startCol}
		}
	case '.':
		if l.peekChar() == '.' && l.readPos+1 < len(l.input) && l.input[l.readPos+1] == '.' {
			l.readChar()
			l.readChar()
			tok = Token{Type: SPREAD, Literal: "...", Line: startLine, Col: startCol}
		} else {
			tok = Token{Type: DOT, Literal: ".", Line: startLine, Col: startCol}
		}
	case '!':
		if l.peekChar() == '=' {
			l.readChar()
			tok = Token{Type: NEQ, Literal: "!=", Line: startLine, Col: startCol}
		} else {
			tok = Token{Type: BANG, Literal: "!", Line: startLine, Col: startCol}
		}
	case '&':
		if l.peekChar() == '&' {
			l.readChar()
			tok = Token{Type: AND, Literal: "&&", Line: startLine, Col: startCol}
		} else {
			tok = Token{Type: ILLEGAL, Literal: string(l.ch), Line: startLine, Col: startCol}
		}
	case '+':
		if l.peekChar() == '=' {
			l.readChar()
			tok = Token{Type: PLUS_ASSIGN, Literal: "+=", Line: startLine, Col: startCol}
		} else {
			tok = Token{Type: PLUS, Literal: "+", Line: startLine, Col: startCol}
		}
	case '*':
		tok = Token{Type: STAR, Literal: "*", Line: startLine, Col: startCol}
	case '/':
		if l.peekChar() == '/' {
			l.skipSingleLineComment()
			return l.NextToken()
		} else if l.peekChar() == '*' {
			l.skipMultiLineComment()
			return l.NextToken()
		} else {
			tok = Token{Type: SLASH, Literal: "/", Line: startLine, Col: startCol}
		}
	case '%':
		tok = Token{Type: PERCENT, Literal: "%", Line: startLine, Col: startCol}
	case '(':
		tok = Token{Type: LPAREN, Literal: "(", Line: startLine, Col: startCol}
	case ')':
		tok = Token{Type: RPAREN, Literal: ")", Line: startLine, Col: startCol}
	case '{':
		tok = Token{Type: LBRACE, Literal: "{", Line: startLine, Col: startCol}
	case '}':
		tok = Token{Type: RBRACE, Literal: "}", Line: startLine, Col: startCol}
	case '[':
		tok = Token{Type: LBRACKET, Literal: "[", Line: startLine, Col: startCol}
	case ']':
		tok = Token{Type: RBRACKET, Literal: "]", Line: startLine, Col: startCol}
	case ',':
		tok = Token{Type: COMMA, Literal: ",", Line: startLine, Col: startCol}
	case ':':
		tok = Token{Type: COLON, Literal: ":", Line: startLine, Col: startCol}
	case ';':
		tok = Token{Type: SEMICOLON, Literal: ";", Line: startLine, Col: startCol}
	case '@':
		tok = Token{Type: AT, Literal: "@", Line: startLine, Col: startCol}
	case '"':
		lit := l.readString()
		tok = Token{Type: STRING_LIT, Literal: lit, Line: startLine, Col: startCol}
		return tok
	default:
		if isLetter(l.ch) {
			literal := l.readIdentifier()
			tokType := LookupIdent(literal)
			tok = Token{Type: tokType, Literal: literal, Line: startLine, Col: startCol}
			return tok
		} else if isDigit(l.ch) {
			literal, tokType := l.readNumber()
			tok = Token{Type: tokType, Literal: literal, Line: startLine, Col: startCol}
			return tok
		} else {
			tok = Token{Type: ILLEGAL, Literal: string(l.ch), Line: startLine, Col: startCol}
		}
	}

	l.readChar()
	return tok
}

// skipWhitespace skips spaces and tabs but not newlines (newlines are tokens).
func (l *Lexer) skipWhitespace() {
	for l.ch == ' ' || l.ch == '\t' || l.ch == '\r' {
		l.readChar()
	}
}

func (l *Lexer) skipSingleLineComment() {
	for l.ch != '\n' && l.ch != 0 {
		l.readChar()
	}
}

// skipMultiLineComment does not nest /* */.
func (l *Lexer) skipMultiLineComment() {
	l.readChar()
	l.readChar()
	for {
		if l.ch == 0 {
			return
		}
		if l.ch == '\n' {
			l.line++
			l.col = 0
		}
		if l.ch == '*' && l.peekChar() == '/' {
			l.readChar()
			l.readChar()
			return
		}
		l.readChar()
	}
}

func (l *Lexer) readIdentifier() string {
	start := l.pos
	for isLetter(l.ch) || isDigit(l.ch) {
		l.readChar()
	}
	return l.input[start:l.pos]
}

// readNumber lexes int/float/duration. Duration suffix requires a recognized
// unit (ns/us/ms/s/m/h) followed by a non-identifier boundary, so `1seconds`
// lexes as INT(1) + IDENT(seconds), not a malformed duration.
func (l *Lexer) readNumber() (string, TokenType) {
	start := l.pos
	for isDigit(l.ch) {
		l.readChar()
	}
	isFloat := false
	if l.ch == '.' && isDigit(l.peekChar()) {
		l.readChar()
		for isDigit(l.ch) {
			l.readChar()
		}
		isFloat = true
	}
	if suffixLen := l.peekDurationSuffix(); suffixLen > 0 {
		for range suffixLen {
			l.readChar()
		}
		return l.input[start:l.pos], DURATION_LIT
	}
	if isFloat {
		return l.input[start:l.pos], FLOAT_LIT
	}
	return l.input[start:l.pos], INT_LIT
}

// peekDurationSuffix returns the unit length (1 for s/m/h, 2 for ns/us/ms),
// or 0 if not followed by a word boundary.
func (l *Lexer) peekDurationSuffix() int {
	if l.pos >= len(l.input) {
		return 0
	}
	ch := l.ch
	next := byte(0)
	if l.pos+1 < len(l.input) {
		next = l.input[l.pos+1]
	}
	// Two-char (ns/us/ms): boundary check prevents `1msx` → "1ms"+"x".
	if (ch == 'n' || ch == 'u' || ch == 'm') && next == 's' {
		boundary := byte(0)
		if l.pos+2 < len(l.input) {
			boundary = l.input[l.pos+2]
		}
		if !isLetter(boundary) && !isDigit(boundary) {
			return 2
		}
	}
	// Single-char (s/m/h): same boundary check.
	if ch == 's' || ch == 'm' || ch == 'h' {
		if !isLetter(next) && !isDigit(next) {
			return 1
		}
	}
	return 0
}

// readString handles escapes: \" \\ \n \t \r. Unknown escapes are kept verbatim.
func (l *Lexer) readString() string {
	l.readChar()
	var buf []byte
	for l.ch != '"' && l.ch != 0 {
		if l.ch == '\\' {
			l.readChar()
			switch l.ch {
			case '"':
				buf = append(buf, '"')
			case '\\':
				buf = append(buf, '\\')
			case 'n':
				buf = append(buf, '\n')
			case 't':
				buf = append(buf, '\t')
			case 'r':
				buf = append(buf, '\r')
			default:
				buf = append(buf, '\\', l.ch)
			}
		} else {
			buf = append(buf, l.ch)
		}
		l.readChar()
	}
	l.readChar()
	return string(buf)
}

func isLetter(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

// CollectAll drains the lexer into a slice including the terminating EOF.
func CollectAll(l *Lexer) []Token {
	var tokens []Token
	for {
		tok := l.NextToken()
		tokens = append(tokens, tok)
		if tok.Type == EOF {
			break
		}
	}
	return tokens
}

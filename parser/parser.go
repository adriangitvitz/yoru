package parser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adriangitvitz/yoru/lexer"
)

// Precedence levels for the Pratt parser.
const (
	_ int = iota
	LOWEST
	SEND_PREC    // <-
	NULLCOALESCE // ??
	OR_PREC      // ||
	AND_PREC     // &&
	EQUALS       // == !=
	LESSGREATER  // < > <= >=
	SUM          // + -
	PRODUCT      // * / %
	PREFIX       // -x !x
	POSTFIX      // x?
	CALL         // fn(x)
	FIELD        // obj.field obj[idx]
)

var precedences = map[lexer.TokenType]int{
	lexer.SEND_OP:         SEND_PREC,
	lexer.DOUBLE_QUESTION: NULLCOALESCE,
	lexer.OR:              OR_PREC,
	lexer.AND:             AND_PREC,
	lexer.EQ:              EQUALS,
	lexer.NEQ:             EQUALS,
	lexer.LT:              LESSGREATER,
	lexer.GT:              LESSGREATER,
	lexer.LTE:             LESSGREATER,
	lexer.GTE:             LESSGREATER,
	lexer.PLUS:            SUM,
	lexer.MINUS:           SUM,
	lexer.STAR:            PRODUCT,
	lexer.SLASH:           PRODUCT,
	lexer.PERCENT:         PRODUCT,
	lexer.QUESTION:        POSTFIX,
	lexer.LPAREN:          CALL,
	lexer.DOT:             FIELD,
	lexer.LBRACKET:        FIELD,
	lexer.ASSIGN:          SEND_PREC,
	lexer.PLUS_ASSIGN:     SEND_PREC,
	lexer.MINUS_ASSIGN:    SEND_PREC,
}

type (
	prefixParseFn func() Expression
	infixParseFn  func(Expression) Expression
)

// Parser produces an AST from a lexer token stream.
type Parser struct {
	l         *lexer.Lexer
	curToken  lexer.Token
	peekToken lexer.Token
	errors    []string

	prefixParseFns map[lexer.TokenType]prefixParseFn
	infixParseFns  map[lexer.TokenType]infixParseFn
}

func New(l *lexer.Lexer) *Parser {
	p := &Parser{
		l:              l,
		errors:         []string{},
		prefixParseFns: make(map[lexer.TokenType]prefixParseFn),
		infixParseFns:  make(map[lexer.TokenType]infixParseFn),
	}

	p.registerPrefix(lexer.IDENT, p.parseIdentifier)
	p.registerPrefix(lexer.INT_LIT, p.parseIntegerLiteral)
	p.registerPrefix(lexer.FLOAT_LIT, p.parseFloatLiteral)
	p.registerPrefix(lexer.STRING_LIT, p.parseStringLiteral)
	p.registerPrefix(lexer.DURATION_LIT, p.parseDurationLiteral)
	p.registerPrefix(lexer.DOT, p.parseLeadingDotExpression)
	p.registerPrefix(lexer.TRUE, p.parseBooleanLiteral)
	p.registerPrefix(lexer.FALSE, p.parseBooleanLiteral)
	p.registerPrefix(lexer.NIL, p.parseNilLiteral)
	p.registerPrefix(lexer.SELF, p.parseSelfExpression)
	p.registerPrefix(lexer.BANG, p.parsePrefixExpression)
	p.registerPrefix(lexer.MINUS, p.parsePrefixExpression)
	p.registerPrefix(lexer.LPAREN, p.parseGroupedExpression)
	p.registerPrefix(lexer.LBRACKET, p.parseListLiteral)
	p.registerPrefix(lexer.LBRACE, p.parseBlockExpression)
	p.registerPrefix(lexer.IF, p.parseIfExpression)
	p.registerPrefix(lexer.MATCH, p.parseMatchExpression)
	p.registerPrefix(lexer.FN, p.parseLambdaExpression)
	p.registerPrefix(lexer.SPAWN, p.parseSpawnExpression)
	p.registerPrefix(lexer.HANDLE, p.parseHandleExpression)
	p.registerPrefix(lexer.FOR, p.parseForInExpression)
	p.registerPrefix(lexer.SEND, p.parseKeywordAsIdentifier)
	p.registerPrefix(lexer.RECEIVE, p.parseKeywordAsIdentifier)
	p.registerPrefix(lexer.EMIT, p.parseKeywordAsIdentifier)
	p.registerPrefix(lexer.YIELD, p.parseKeywordAsIdentifier)
	p.registerPrefix(lexer.SPREAD, p.parseSpreadExpression)
	p.registerPrefix(lexer.SUPER, p.parseSuperExpression)
	p.registerPrefix(lexer.WHILE, p.parseWhileExpression)

	p.registerInfix(lexer.PLUS, p.parseInfixExpression)
	p.registerInfix(lexer.MINUS, p.parseInfixExpression)
	p.registerInfix(lexer.STAR, p.parseInfixExpression)
	p.registerInfix(lexer.SLASH, p.parseInfixExpression)
	p.registerInfix(lexer.PERCENT, p.parseInfixExpression)
	p.registerInfix(lexer.EQ, p.parseInfixExpression)
	p.registerInfix(lexer.NEQ, p.parseInfixExpression)
	p.registerInfix(lexer.LT, p.parseInfixExpression)
	p.registerInfix(lexer.GT, p.parseInfixExpression)
	p.registerInfix(lexer.LTE, p.parseInfixExpression)
	p.registerInfix(lexer.GTE, p.parseInfixExpression)
	p.registerInfix(lexer.AND, p.parseInfixExpression)
	p.registerInfix(lexer.OR, p.parseInfixExpression)
	p.registerInfix(lexer.SEND_OP, p.parseInfixExpression)
	p.registerInfix(lexer.DOUBLE_QUESTION, p.parseInfixExpression)
	p.registerInfix(lexer.ASSIGN, p.parseAssignmentExpression)
	p.registerInfix(lexer.PLUS_ASSIGN, p.parseAssignmentExpression)
	p.registerInfix(lexer.MINUS_ASSIGN, p.parseAssignmentExpression)
	p.registerInfix(lexer.DOT, p.parseFieldAccessExpression)
	p.registerInfix(lexer.LPAREN, p.parseCallExpression)
	p.registerInfix(lexer.LBRACKET, p.parseIndexExpression)
	p.registerInfix(lexer.QUESTION, p.parsePostfixExpression)

	// Prime curToken and peekToken.
	p.nextToken()
	p.nextToken()

	return p
}

func (p *Parser) Errors() []string {
	return p.errors
}

// ParseProgram returns the AST root for the full token stream.
func (p *Parser) ParseProgram() *Program {
	program := &Program{}

	for p.curToken.Type != lexer.EOF {
		stmt := p.parseStatement()
		if stmt != nil {
			program.Statements = append(program.Statements, stmt)
		}
		p.nextToken()
	}

	return program
}

func (p *Parser) nextToken() {
	p.curToken = p.peekToken
	for {
		p.peekToken = p.l.NextToken()
		if p.peekToken.Type != lexer.NEWLINE {
			break
		}
	}
}

func (p *Parser) curTokenIs(t lexer.TokenType) bool {
	return p.curToken.Type == t
}

func (p *Parser) peekTokenIs(t lexer.TokenType) bool {
	return p.peekToken.Type == t
}

func (p *Parser) expectPeek(t lexer.TokenType) bool {
	if p.peekTokenIs(t) {
		p.nextToken()
		return true
	}
	p.peekError(t)
	return false
}

func (p *Parser) peekError(t lexer.TokenType) {
	msg := fmt.Sprintf("expected next token to be %s, got %s instead (line %d, col %d)",
		t, p.peekToken.Type, p.peekToken.Line, p.peekToken.Col)
	p.errors = append(p.errors, msg)
}

func (p *Parser) noPrefixParseFnError(t lexer.TokenType) {
	msg := fmt.Sprintf("no prefix parse function for %s found (line %d, col %d)",
		t, p.curToken.Line, p.curToken.Col)
	p.errors = append(p.errors, msg)
}

func (p *Parser) registerPrefix(tokenType lexer.TokenType, fn prefixParseFn) {
	p.prefixParseFns[tokenType] = fn
}

func (p *Parser) registerInfix(tokenType lexer.TokenType, fn infixParseFn) {
	p.infixParseFns[tokenType] = fn
}

func (p *Parser) peekPrecedence() int {
	if p, ok := precedences[p.peekToken.Type]; ok {
		return p
	}
	return LOWEST
}

func (p *Parser) curPrecedence() int {
	if p, ok := precedences[p.curToken.Type]; ok {
		return p
	}
	return LOWEST
}

func (p *Parser) parseStatement() Statement {
	switch p.curToken.Type {
	case lexer.LET:
		return p.parseLetStatement()
	case lexer.MUT:
		return p.parseMutStatement()
	case lexer.RETURN:
		return p.parseReturnStatement()
	case lexer.BREAK:
		// Leave curToken AT `break`; the enclosing block's loop will advance.
		return &BreakStatement{Token: p.curToken}
	case lexer.CONTINUE:
		return &ContinueStatement{Token: p.curToken}
	case lexer.FN:
		// fn as declaration (named) vs expression (lambda) — check peek for IDENT
		if p.peekTokenIs(lexer.IDENT) {
			return p.parseFnDecl()
		}
		return p.parseExpressionStatement()
	case lexer.OBJECT:
		return p.parseObjectDecl()
	case lexer.ENUM:
		return p.parseEnumDecl()
	case lexer.ACTOR:
		return p.parseActorDecl()
	case lexer.PIPELINE:
		return p.parsePipelineDecl()
	case lexer.TOOL:
		return p.parseToolDecl()
	case lexer.AGENT:
		return p.parseAgentDecl()
	case lexer.MCP:
		return p.parseMCPDecl()
	case lexer.SERVICE:
		return p.parseServiceDecl()
	case lexer.EFFECT:
		// effect Name { ... } — custom effect declaration
		if p.peekTokenIs(lexer.IDENT) {
			return p.parseEffectDecl()
		}
		return p.parseExpressionStatement()
	case lexer.PROTOCOL:
		return p.parseProtocolDecl()
	case lexer.IMPL:
		return p.parseImplDecl()
	case lexer.TYPE:
		return p.parseTypeAliasDecl()
	case lexer.IMPORT:
		return p.parseImportStatement()
	case lexer.EXPORT:
		return p.parseExportStatement()
	default:
		return p.parseExpressionStatement()
	}
}

func (p *Parser) parseLetStatement() Statement {
	letToken := p.curToken

	// Check for destructure: let [a, b, ...rest] = expr
	if p.peekTokenIs(lexer.LBRACKET) {
		return p.parseLetDestructure(letToken)
	}

	stmt := &LetStatement{Token: letToken}

	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	stmt.Name = p.curToken.Literal

	if p.peekTokenIs(lexer.COLON) {
		p.nextToken()
		p.nextToken()

		if p.isCapability(p.curToken.Type) {
			stmt.Capability = p.curToken.Literal
			p.nextToken()
		}
		stmt.TypeExpr = p.parseTypeExpr()
	}

	if !p.expectPeek(lexer.ASSIGN) {
		return nil
	}
	p.nextToken()

	stmt.Value = p.parseExpression(LOWEST)

	if p.peekTokenIs(lexer.SEMICOLON) {
		p.nextToken()
	}

	return stmt
}

func (p *Parser) parseMutStatement() *MutStatement {
	stmt := &MutStatement{Token: p.curToken}

	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	stmt.Name = p.curToken.Literal

	if p.peekTokenIs(lexer.COLON) {
		p.nextToken()
		p.nextToken()

		if p.isCapability(p.curToken.Type) {
			stmt.Capability = p.curToken.Literal
			p.nextToken()
		}
		stmt.TypeExpr = p.parseTypeExpr()
	}

	if !p.expectPeek(lexer.ASSIGN) {
		return nil
	}
	p.nextToken()

	stmt.Value = p.parseExpression(LOWEST)

	if p.peekTokenIs(lexer.SEMICOLON) {
		p.nextToken()
	}

	return stmt
}

func (p *Parser) parseReturnStatement() *ReturnStatement {
	stmt := &ReturnStatement{Token: p.curToken}
	p.nextToken()

	stmt.Value = p.parseExpression(LOWEST)

	if p.peekTokenIs(lexer.SEMICOLON) {
		p.nextToken()
	}

	return stmt
}

func (p *Parser) parseExpressionStatement() *ExpressionStatement {
	stmt := &ExpressionStatement{Token: p.curToken}
	stmt.Expression = p.parseExpression(LOWEST)

	if p.peekTokenIs(lexer.SEMICOLON) {
		p.nextToken()
	}

	return stmt
}

// parseExpression is the Pratt-parser entry point: pick a prefix function
// for the current token, then keep consuming infix operators that bind at
// least as tight as `precedence`.
func (p *Parser) parseExpression(precedence int) Expression {
	prefix := p.prefixParseFns[p.curToken.Type]
	if prefix == nil {
		p.noPrefixParseFnError(p.curToken.Type)
		return nil
	}
	leftExp := prefix()

	for !p.peekTokenIs(lexer.SEMICOLON) && !p.peekTokenIs(lexer.EOF) && precedence < p.peekPrecedence() {
		infix := p.infixParseFns[p.peekToken.Type]
		if infix == nil {
			return leftExp
		}
		p.nextToken()
		leftExp = infix(leftExp)
	}

	return leftExp
}

func (p *Parser) parseIdentifier() Expression {
	ident := &Identifier{Token: p.curToken, Value: p.curToken.Literal}

	// Check for object literal: UpperCase identifier followed by {
	if len(ident.Value) > 0 && ident.Value[0] >= 'A' && ident.Value[0] <= 'Z' && p.peekTokenIs(lexer.LBRACE) {
		return p.parseObjectLiteral(ident.Value)
	}

	return ident
}

func (p *Parser) parseObjectLiteral(name string) Expression {
	tok := p.curToken
	p.nextToken()

	ol := &ObjectLiteral{Token: tok, Name: name}

	if p.peekTokenIs(lexer.RBRACE) {
		p.nextToken()
		return ol
	}

	for {
		p.nextToken()
		if p.curTokenIs(lexer.RBRACE) || p.curTokenIs(lexer.EOF) {
			break
		}

		fieldName := p.curToken.Literal

		if !p.expectPeek(lexer.COLON) {
			return nil
		}
		p.nextToken()

		value := p.parseExpression(LOWEST)
		ol.Fields = append(ol.Fields, ObjectLiteralField{Name: fieldName, Value: value})

		// After parsing a value expression (e.g. a lambda), curToken may be at
		// the inner expression's closing '}'. Check peekToken to decide whether
		// we've reached the end of the ObjectLiteral.
		if p.peekTokenIs(lexer.COMMA) {
			p.nextToken()
			continue
		}
		if p.peekTokenIs(lexer.RBRACE) {
			p.nextToken()
			break
		}
		// If peek is neither comma nor rbrace, the next iteration will handle it
	}

	return ol
}

func (p *Parser) parseIntegerLiteral() Expression {
	lit := &IntegerLiteral{Token: p.curToken}
	value, err := strconv.ParseInt(p.curToken.Literal, 10, 64)
	if err != nil {
		msg := fmt.Sprintf("could not parse %q as integer", p.curToken.Literal)
		p.errors = append(p.errors, msg)
		return nil
	}
	lit.Value = value
	return lit
}

// parseDurationLiteral lowers `60s` / `500ms` / `1.5h` to an IntegerLiteral
// of canonical milliseconds; downstream sees a plain Int.
func (p *Parser) parseDurationLiteral() Expression {
	tok := p.curToken
	millis, err := durationLiteralToMillis(tok.Literal)
	if err != nil {
		p.errors = append(p.errors,
			fmt.Sprintf("invalid duration literal %q: %s (line %d, col %d)",
				tok.Literal, err.Error(), tok.Line, tok.Col))
		return nil
	}
	return &IntegerLiteral{Token: tok, Value: millis}
}

// durationLiteralToMillis splits `<number><unit>` and returns ms. Unit
// must be one of ns, us, ms, s, m, h (matches the lexer's accepted set).
func durationLiteralToMillis(lit string) (int64, error) {
	i := 0
	for i < len(lit) && (lit[i] >= '0' && lit[i] <= '9' || lit[i] == '.') {
		i++
	}
	if i == 0 || i == len(lit) {
		return 0, fmt.Errorf("missing numeric prefix or unit")
	}
	numStr, unit := lit[:i], lit[i:]
	num, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, err
	}
	switch unit {
	case "ns":
		return int64(num / 1e6), nil
	case "us":
		return int64(num / 1e3), nil
	case "ms":
		return int64(num), nil
	case "s":
		return int64(num * 1000), nil
	case "m":
		return int64(num * 60 * 1000), nil
	case "h":
		return int64(num * 3600 * 1000), nil
	}
	return 0, fmt.Errorf("unknown unit %q", unit)
}

// parseLeadingDotExpression handles `.name` or `.name(args...)` in expression
// position. See LeadingDotExpression for semantics.
func (p *Parser) parseLeadingDotExpression() Expression {
	tok := p.curToken // DOT
	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	expr := &LeadingDotExpression{Token: tok, Name: p.curToken.Literal}

	if !p.peekTokenIs(lexer.LPAREN) {
		return expr
	}
	p.nextToken()
	if p.peekTokenIs(lexer.RPAREN) {
		p.nextToken()
		return expr
	}
	for {
		p.nextToken()
		arg := CallArg{}
		if p.curTokenIs(lexer.IDENT) && p.peekTokenIs(lexer.COLON) {
			arg.Name = p.curToken.Literal
			p.nextToken()
			p.nextToken()
		}
		arg.Value = p.parseExpression(LOWEST)
		expr.Args = append(expr.Args, arg)
		if p.peekTokenIs(lexer.COMMA) {
			p.nextToken()
			continue
		}
		break
	}
	if !p.expectPeek(lexer.RPAREN) {
		return nil
	}
	return expr
}

func (p *Parser) parseFloatLiteral() Expression {
	lit := &FloatLiteral{Token: p.curToken}
	value, err := strconv.ParseFloat(p.curToken.Literal, 64)
	if err != nil {
		msg := fmt.Sprintf("could not parse %q as float", p.curToken.Literal)
		p.errors = append(p.errors, msg)
		return nil
	}
	lit.Value = value
	return lit
}

func (p *Parser) parseStringLiteral() Expression {
	return &StringLiteral{Token: p.curToken, Value: p.curToken.Literal}
}

func (p *Parser) parseBooleanLiteral() Expression {
	return &BooleanLiteral{Token: p.curToken, Value: p.curTokenIs(lexer.TRUE)}
}

func (p *Parser) parseNilLiteral() Expression {
	return &NilLiteral{Token: p.curToken}
}

func (p *Parser) parseSelfExpression() Expression {
	return &SelfExpression{Token: p.curToken}
}

func (p *Parser) parseKeywordAsIdentifier() Expression {
	return &Identifier{Token: p.curToken, Value: p.curToken.Literal}
}

func (p *Parser) parsePrefixExpression() Expression {
	expr := &PrefixExpression{Token: p.curToken, Operator: p.curToken.Literal}
	p.nextToken()
	expr.Right = p.parseExpression(PREFIX)
	return expr
}

func (p *Parser) parseGroupedExpression() Expression {
	p.nextToken()
	exp := p.parseExpression(LOWEST)
	if !p.expectPeek(lexer.RPAREN) {
		return nil
	}
	return exp
}

func (p *Parser) parseListLiteral() Expression {
	lit := &ListLiteral{Token: p.curToken}
	lit.Elements = p.parseExpressionList(lexer.RBRACKET)
	return lit
}

func (p *Parser) parseBlockExpression() Expression {
	tok := p.curToken // LBRACE
	p.nextToken()

	// Disambiguate `{ ident : ... }` as a bare ObjectLiteral with empty Name.
	// `{}` and `{ stmt; ... }` continue to be BlockExpression.
	if p.curTokenIs(lexer.IDENT) && p.peekTokenIs(lexer.COLON) {
		return p.parseBareObjectLiteralBody(tok)
	}

	block := &BlockExpression{Token: tok}
	for !p.curTokenIs(lexer.RBRACE) && !p.curTokenIs(lexer.EOF) {
		stmt := p.parseStatement()
		if stmt != nil {
			block.Statements = append(block.Statements, stmt)
		}
		p.nextToken()
	}

	return block
}

// parseBareObjectLiteralBody parses fields of an unnamed object literal,
// starting with curToken positioned at the first field name.
func (p *Parser) parseBareObjectLiteralBody(open lexer.Token) Expression {
	ol := &ObjectLiteral{Token: open, Name: ""}
	for {
		if !p.curTokenIs(lexer.IDENT) {
			p.errors = append(p.errors,
				fmt.Sprintf("expected field name in object literal, got %s (line %d, col %d)",
					p.curToken.Type, p.curToken.Line, p.curToken.Col))
			return nil
		}
		fieldName := p.curToken.Literal
		if !p.expectPeek(lexer.COLON) {
			return nil
		}
		p.nextToken()
		value := p.parseExpression(LOWEST)
		ol.Fields = append(ol.Fields, ObjectLiteralField{Name: fieldName, Value: value})

		if p.peekTokenIs(lexer.COMMA) {
			p.nextToken()
			p.nextToken()
			continue
		}
		if p.peekTokenIs(lexer.RBRACE) {
			p.nextToken()
			return ol
		}
		// Trailing comma already-consumed-and-empty case
		if p.curTokenIs(lexer.RBRACE) {
			return ol
		}
		p.errors = append(p.errors,
			fmt.Sprintf("expected ',' or '}' in object literal, got %s (line %d, col %d)",
				p.peekToken.Type, p.peekToken.Line, p.peekToken.Col))
		return nil
	}
}

func (p *Parser) parseIfExpression() Expression {
	expr := &IfExpression{Token: p.curToken}
	p.nextToken()

	expr.Condition = p.parseExpression(LOWEST)

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}
	expr.Consequence = p.parseBlockExpressionBody()

	if p.peekTokenIs(lexer.ELSE) {
		p.nextToken()
		// `else if`: wrap the chained if in a single-stmt block so Alternative
		// is always *BlockExpression.
		if p.peekTokenIs(lexer.IF) {
			p.nextToken()
			chained := p.parseIfExpression()
			if chained == nil {
				return nil
			}
			expr.Alternative = &BlockExpression{
				Token: p.curToken,
				Statements: []Statement{
					&ExpressionStatement{Token: p.curToken, Expression: chained},
				},
			}
		} else {
			if !p.expectPeek(lexer.LBRACE) {
				return nil
			}
			expr.Alternative = p.parseBlockExpressionBody()
		}
	}

	return expr
}

func (p *Parser) parseBlockExpressionBody() *BlockExpression {
	block := &BlockExpression{Token: p.curToken}
	p.nextToken()

	for !p.curTokenIs(lexer.RBRACE) && !p.curTokenIs(lexer.EOF) {
		stmt := p.parseStatement()
		if stmt != nil {
			block.Statements = append(block.Statements, stmt)
		}
		p.nextToken()
	}

	return block
}

func (p *Parser) parseMatchExpression() Expression {
	expr := &MatchExpression{Token: p.curToken}
	p.nextToken()

	expr.Subject = p.parseExpression(LOWEST)

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}
	p.nextToken()

	for !p.curTokenIs(lexer.RBRACE) && !p.curTokenIs(lexer.EOF) {
		arm := p.parseMatchArm()
		expr.Arms = append(expr.Arms, arm)

		if p.peekTokenIs(lexer.COMMA) {
			p.nextToken()
		}
		p.nextToken()
	}

	return expr
}

func (p *Parser) parseMatchArm() MatchArm {
	arm := MatchArm{}
	arm.Pattern = p.parseMatchPattern()

	// Optional guard: `pattern if condition => body`
	if p.peekTokenIs(lexer.IF) {
		p.nextToken()
		p.nextToken()
		arm.Guard = p.parseExpression(LOWEST)
	}

	if !p.expectPeek(lexer.FAT_ARROW) {
		return arm
	}
	p.nextToken()
	arm.Body = p.parseExpression(LOWEST)

	return arm
}

func (p *Parser) parseMatchPattern() MatchPattern {
	pat := MatchPattern{}

	switch {
	case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "_":
		pat.Kind = "wildcard"
		pat.Value = "_"

	case p.curTokenIs(lexer.INT_LIT) || p.curTokenIs(lexer.FLOAT_LIT):
		pat.Kind = "literal"
		pat.Value = p.curToken.Literal

	case p.curTokenIs(lexer.STRING_LIT):
		pat.Kind = "literal"
		pat.Value = p.curToken.Literal

	case p.curTokenIs(lexer.IDENT):
		var name strings.Builder
		name.WriteString(p.curToken.Literal)

		// Check for qualified path: Ident.Ident
		for p.peekTokenIs(lexer.DOT) {
			p.nextToken()
			p.nextToken()
			name.WriteString("." + p.curToken.Literal)
		}

		// Check for destructuring: Name(binding, ...)
		if p.peekTokenIs(lexer.LPAREN) {
			pat.Kind = "destructure"
			pat.Value = name.String()
			p.nextToken()
			for !p.peekTokenIs(lexer.RPAREN) && !p.peekTokenIs(lexer.EOF) {
				p.nextToken()
				pat.Bindings = append(pat.Bindings, p.curToken.Literal)
				if p.peekTokenIs(lexer.COMMA) {
					p.nextToken()
				}
			}
			p.nextToken()
		} else if p.peekTokenIs(lexer.LBRACE) {
			// Object-style destructure: Name { field } or Name { field: pattern }
			pat.Kind = "object_destructure"
			pat.Value = name.String()
			p.nextToken()
			for !p.peekTokenIs(lexer.RBRACE) && !p.peekTokenIs(lexer.EOF) {
				p.nextToken()
				if !p.curTokenIs(lexer.IDENT) {
					p.errors = append(p.errors,
						fmt.Sprintf("expected field name inside object pattern, got %s (line %d, col %d)",
							p.curToken.Type, p.curToken.Line, p.curToken.Col))
					return pat
				}
				field := ObjectFieldPattern{Field: p.curToken.Literal}
				if p.peekTokenIs(lexer.COLON) {
					p.nextToken()
					p.nextToken()
					sub := p.parseMatchPattern()
					field.Subpattern = &sub
				}
				pat.ObjectFields = append(pat.ObjectFields, field)
				if p.peekTokenIs(lexer.COMMA) {
					p.nextToken()
				}
			}
			p.nextToken()
		} else {
			pat.Kind = "identifier"
			pat.Value = name.String()
		}

	default:
		pat.Kind = "identifier"
		pat.Value = p.curToken.Literal
	}

	return pat
}

func (p *Parser) parseLambdaExpression() Expression {
	le := &LambdaExpression{Token: p.curToken}

	if !p.expectPeek(lexer.LPAREN) {
		return nil
	}
	le.Params = p.parseFnParams()

	if p.peekTokenIs(lexer.ARROW) {
		p.nextToken()
		p.nextToken()
		le.ReturnType = p.parseTypeExpr()
	}

	// Arrow shorthand: fn(x) => expr  or  fn(x) -> T => expr
	if p.peekTokenIs(lexer.FAT_ARROW) {
		p.nextToken()
		p.nextToken()
		bodyExpr := p.parseExpression(LOWEST)
		le.Body = &BlockExpression{
			Token: p.curToken,
			Statements: []Statement{
				&ExpressionStatement{Token: p.curToken, Expression: bodyExpr},
			},
		}
		return le
	}

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}
	le.Body = p.parseBlockExpressionBody()

	return le
}

func (p *Parser) parseSpawnExpression() Expression {
	expr := &SpawnExpression{Token: p.curToken}
	p.nextToken()
	expr.Call = p.parseExpression(LOWEST)
	return expr
}

func (p *Parser) parseHandleExpression() Expression {
	expr := &HandleExpression{Token: p.curToken}

	if !p.expectPeek(lexer.LPAREN) {
		return nil
	}
	p.nextToken()
	expr.EffectName = p.curToken.Literal
	if !p.expectPeek(lexer.RPAREN) {
		return nil
	}

	// { using: handler_expr }
	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}
	p.nextToken()
	if p.curToken.Literal != "using" {
		p.errors = append(p.errors, fmt.Sprintf("expected 'using' in handle block, got %s", p.curToken.Literal))
		return nil
	}
	if !p.expectPeek(lexer.COLON) {
		return nil
	}
	p.nextToken()
	expr.Handler = p.parseExpression(LOWEST)

	if !p.expectPeek(lexer.RBRACE) {
		return nil
	}

	if !p.expectPeek(lexer.IN) {
		return nil
	}

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}
	expr.Body = p.parseBlockExpressionBody()

	return expr
}

func (p *Parser) parseForInExpression() Expression {
	expr := &ForInExpression{Token: p.curToken}

	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	expr.Variable = p.curToken.Literal

	if !p.expectPeek(lexer.IN) {
		return nil
	}
	p.nextToken()

	expr.Iterable = p.parseExpression(LOWEST)

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}
	expr.Body = p.parseBlockExpressionBody()

	return expr
}

func (p *Parser) parseWhileExpression() Expression {
	expr := &WhileExpression{Token: p.curToken}
	p.nextToken()

	expr.Condition = p.parseExpression(LOWEST)

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}
	expr.Body = p.parseBlockExpressionBody()

	return expr
}

func (p *Parser) parseInfixExpression(left Expression) Expression {
	expr := &InfixExpression{
		Token:    p.curToken,
		Left:     left,
		Operator: p.curToken.Literal,
	}
	precedence := p.curPrecedence()
	p.nextToken()
	expr.Right = p.parseExpression(precedence)
	return expr
}

func (p *Parser) parseAssignmentExpression(left Expression) Expression {
	expr := &AssignmentExpression{
		Token:    p.curToken,
		Left:     left,
		Operator: p.curToken.Literal,
	}
	p.nextToken()
	expr.Value = p.parseExpression(LOWEST)
	return expr
}

func (p *Parser) parseFieldAccessExpression(left Expression) Expression {
	expr := &FieldAccessExpression{Token: p.curToken, Left: left}
	p.nextToken()
	expr.Field = p.curToken.Literal
	return expr
}

func (p *Parser) parseCallExpression(function Expression) Expression {
	expr := &CallExpression{Token: p.curToken, Function: function}
	expr.Args = p.parseCallArgs()
	return expr
}

func (p *Parser) parseCallArgs() []CallArg {
	var args []CallArg

	if p.peekTokenIs(lexer.RPAREN) {
		p.nextToken()
		return args
	}

	p.nextToken()

	for {
		arg := CallArg{}

		// Check for named arg: ident COLON expr
		if p.curTokenIs(lexer.IDENT) && p.peekTokenIs(lexer.COLON) {
			arg.Name = p.curToken.Literal
			p.nextToken()
			p.nextToken()
		}

		arg.Value = p.parseExpression(LOWEST)
		args = append(args, arg)

		if !p.peekTokenIs(lexer.COMMA) {
			break
		}
		p.nextToken()
		p.nextToken()
	}

	if !p.expectPeek(lexer.RPAREN) {
		return nil
	}
	return args
}

func (p *Parser) parseIndexExpression(left Expression) Expression {
	expr := &IndexExpression{Token: p.curToken, Left: left}
	p.nextToken()
	expr.Index = p.parseExpression(LOWEST)
	if !p.expectPeek(lexer.RBRACKET) {
		return nil
	}
	return expr
}

func (p *Parser) parsePostfixExpression(left Expression) Expression {
	return &PostfixExpression{
		Token:    p.curToken,
		Left:     left,
		Operator: p.curToken.Literal,
	}
}

func (p *Parser) parseObjectDecl() *ObjectDecl {
	decl := &ObjectDecl{Token: p.curToken}

	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	decl.Name = p.curToken.Literal

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}

	p.nextToken()
	for !p.curTokenIs(lexer.RBRACE) && !p.curTokenIs(lexer.EOF) {
		if p.curTokenIs(lexer.DELEGATE) {
			// delegate name: Type
			if !p.expectPeek(lexer.IDENT) {
				return nil
			}
			name := p.curToken.Literal
			if !p.expectPeek(lexer.COLON) {
				return nil
			}
			p.nextToken()
			typeExpr := p.parseTypeExpr()
			decl.Delegates = append(decl.Delegates, DelegateField{Name: name, TypeExpr: typeExpr})
			if p.peekTokenIs(lexer.COMMA) {
				p.nextToken()
			}
			p.nextToken()
			continue
		}

		field := Field{}
		field.Name = p.curToken.Literal

		if !p.expectPeek(lexer.COLON) {
			return decl
		}
		p.nextToken()
		field.TypeExpr = p.parseTypeExpr()

		if p.peekTokenIs(lexer.ASSIGN) {
			p.nextToken()
			p.nextToken()
			field.Default = p.parseExpression(LOWEST)
		}

		if p.peekTokenIs(lexer.AT) {
			p.nextToken()
			field.Annotation = p.parseAnnotation()
		}

		if p.peekTokenIs(lexer.COMMA) {
			p.nextToken()
		}

		decl.Fields = append(decl.Fields, field)
		p.nextToken()
	}

	return decl
}

func (p *Parser) parseFieldList() []Field {
	var fields []Field

	p.nextToken()
	for !p.curTokenIs(lexer.RBRACE) && !p.curTokenIs(lexer.EOF) {
		field := Field{}
		field.Name = p.curToken.Literal

		if !p.expectPeek(lexer.COLON) {
			return fields
		}
		p.nextToken()
		field.TypeExpr = p.parseTypeExpr()

		if p.peekTokenIs(lexer.ASSIGN) {
			p.nextToken()
			p.nextToken()
			field.Default = p.parseExpression(LOWEST)
		}

		if p.peekTokenIs(lexer.AT) {
			p.nextToken()
			field.Annotation = p.parseAnnotation()
		}

		if p.peekTokenIs(lexer.COMMA) {
			p.nextToken()
		}

		fields = append(fields, field)
		p.nextToken()
	}

	return fields
}

func (p *Parser) parseAnnotation() *Annotation {
	ann := &Annotation{}
	p.nextToken()
	ann.Name = p.curToken.Literal

	if p.peekTokenIs(lexer.LPAREN) {
		p.nextToken()
		ann.Args = p.parseExpressionList(lexer.RPAREN)
	}

	return ann
}

func (p *Parser) parseEnumDecl() *EnumDecl {
	decl := &EnumDecl{Token: p.curToken}

	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	decl.Name = p.curToken.Literal

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}

	p.nextToken()
	for !p.curTokenIs(lexer.RBRACE) && !p.curTokenIs(lexer.EOF) {
		variant := EnumVariant{Name: p.curToken.Literal}

		if p.peekTokenIs(lexer.LPAREN) {
			p.nextToken()
			variant.Fields = p.parseEnumVariantFields()
		}

		decl.Variants = append(decl.Variants, variant)

		if p.peekTokenIs(lexer.COMMA) {
			p.nextToken()
		}
		p.nextToken()
	}

	return decl
}

func (p *Parser) parseEnumVariantFields() []Field {
	var fields []Field

	p.nextToken()
	for !p.curTokenIs(lexer.RPAREN) && !p.curTokenIs(lexer.EOF) {
		field := Field{Name: p.curToken.Literal}

		if p.peekTokenIs(lexer.COLON) {
			p.nextToken()
			p.nextToken()
			field.TypeExpr = p.parseTypeExpr()
		}

		fields = append(fields, field)

		if p.peekTokenIs(lexer.COMMA) {
			p.nextToken()
		}
		p.nextToken()
	}

	return fields
}

func (p *Parser) parseFnDecl() *FnDecl {
	decl := &FnDecl{Token: p.curToken}

	p.nextToken()
	decl.Name = p.curToken.Literal

	if !p.expectPeek(lexer.LPAREN) {
		return nil
	}
	decl.Params = p.parseFnParams()

	if p.peekTokenIs(lexer.ARROW) {
		p.nextToken()
		p.nextToken()
		decl.ReturnType = p.parseTypeExpr()
	}

	if p.peekTokenIs(lexer.EFFECT) {
		p.nextToken()
		decl.Effects = p.parseEffectList()
	}

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}
	decl.Body = p.parseBlockExpressionBody()

	return decl
}

func (p *Parser) parseFnParams() []Param {
	var params []Param

	if p.peekTokenIs(lexer.RPAREN) {
		p.nextToken()
		return params
	}

	p.nextToken()
	for {
		param := Param{}

		if p.curTokenIs(lexer.SELF) {
			param.Name = "self"
			params = append(params, param)
			if p.peekTokenIs(lexer.COMMA) {
				p.nextToken()
				p.nextToken()
				continue
			}
			break
		}

		param.Name = p.curToken.Literal

		if p.peekTokenIs(lexer.COLON) {
			p.nextToken()
			p.nextToken()

			if p.isCapability(p.curToken.Type) {
				param.Capability = p.curToken.Literal
				p.nextToken()
			}
			param.TypeExpr = p.parseTypeExpr()
		}

		if p.peekTokenIs(lexer.ASSIGN) {
			p.nextToken()
			p.nextToken()
			param.Default = p.parseExpression(LOWEST)
		}

		params = append(params, param)

		if !p.peekTokenIs(lexer.COMMA) {
			break
		}
		p.nextToken()
		p.nextToken()
	}

	if !p.expectPeek(lexer.RPAREN) {
		return nil
	}
	return params
}

func (p *Parser) parseEffectList() []string {
	var effects []string

	if !p.expectPeek(lexer.LBRACKET) {
		return effects
	}

	p.nextToken()
	for !p.curTokenIs(lexer.RBRACKET) && !p.curTokenIs(lexer.EOF) {
		effects = append(effects, p.curToken.Literal)
		if p.peekTokenIs(lexer.COMMA) {
			p.nextToken()
		}
		p.nextToken()
	}

	return effects
}

func (p *Parser) parseActorDecl() *ActorDecl {
	decl := &ActorDecl{Token: p.curToken}

	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	decl.Name = p.curToken.Literal

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}

	p.nextToken()
	for !p.curTokenIs(lexer.RBRACE) && !p.curTokenIs(lexer.EOF) {
		switch {
		case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "state":
			sf := p.parseStateField()
			decl.States = append(decl.States, sf)
		case p.curTokenIs(lexer.RECEIVE):
			rb := p.parseReceiveBlock()
			decl.Receives = append(decl.Receives, rb)
		}
		p.nextToken()
	}

	return decl
}

func (p *Parser) parseStateField() StateField {
	sf := StateField{}
	p.nextToken()
	sf.Name = p.curToken.Literal

	if p.peekTokenIs(lexer.COLON) {
		p.nextToken()
		p.nextToken()
		sf.TypeExpr = p.parseTypeExpr()
	}

	if p.peekTokenIs(lexer.ASSIGN) {
		p.nextToken()
		p.nextToken()
		sf.Default = p.parseExpression(LOWEST)
	}

	return sf
}

func (p *Parser) parseReceiveBlock() ReceiveBlock {
	rb := ReceiveBlock{}
	p.nextToken()
	rb.MessageType = p.curToken.Literal

	// Optional param list: `Add(n: Int)`.
	if p.peekTokenIs(lexer.LPAREN) {
		p.nextToken()
		rb.Params = p.parseFnParams()
	}

	if p.peekTokenIs(lexer.ARROW) {
		p.nextToken()
		p.nextToken()
		rb.ReturnType = p.parseTypeExpr()
	}

	if !p.expectPeek(lexer.LBRACE) {
		return rb
	}
	rb.Body = p.parseBlockExpressionBody()

	return rb
}

func (p *Parser) parsePipelineDecl() *PipelineDecl {
	decl := &PipelineDecl{Token: p.curToken}

	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	decl.Name = p.curToken.Literal

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}

	p.nextToken()
	for !p.curTokenIs(lexer.RBRACE) && !p.curTokenIs(lexer.EOF) {
		stage := PipelineStage{}

		switch {
		case p.curTokenIs(lexer.SOURCE):
			stage.Kind = "source"
		case p.curTokenIs(lexer.TRANSFORM):
			stage.Kind = "transform"
		case p.curTokenIs(lexer.SINK):
			stage.Kind = "sink"
		case p.curTokenIs(lexer.PARTITION):
			stage.Kind = "partition"
		case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "on_error":
			stage.Kind = "on_error"
		case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "back_pressure":
			stage.Kind = "back_pressure"
		case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "checkpoint":
			stage.Kind = "checkpoint"
		case p.curTokenIs(lexer.PIPE_FORWARD):
			// |> prefix before a stage keyword — skip and continue
			p.nextToken()
			continue
		default:
			p.nextToken()
			continue
		}

		if !p.expectPeek(lexer.COLON) {
			return nil
		}
		p.nextToken()
		stage.Expr = p.parseExpression(LOWEST)
		decl.Stages = append(decl.Stages, stage)

		p.nextToken()
	}

	return decl
}

func (p *Parser) parseToolDecl() *ToolDecl {
	decl := &ToolDecl{Token: p.curToken}

	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	decl.Name = p.curToken.Literal

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}

	p.nextToken()
	for !p.curTokenIs(lexer.RBRACE) && !p.curTokenIs(lexer.EOF) {
		switch {
		case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "description":
			if !p.expectPeek(lexer.COLON) {
				return nil
			}
			p.nextToken()
			if sl, ok := p.parseExpression(LOWEST).(*StringLiteral); ok {
				decl.Description = sl.Value
			}

		case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "capability":
			if !p.expectPeek(lexer.COLON) {
				return nil
			}
			if !p.expectPeek(lexer.DOT) {
				return nil
			}
			p.nextToken()
			decl.Capability = p.curToken.Literal

		case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "input":
			if !p.expectPeek(lexer.LBRACE) {
				return nil
			}
			decl.Inputs = p.parseFieldList()

		case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "output":
			// `output: Type` vs `output { fields }` (structured, like input).
			if p.peekTokenIs(lexer.LBRACE) {
				p.nextToken()
				decl.Outputs = p.parseFieldList()
			} else {
				if !p.expectPeek(lexer.COLON) {
					return nil
				}
				p.nextToken()
				decl.OutputType = p.parseTypeExpr()
			}

		case p.curTokenIs(lexer.EFFECT):
			if !p.expectPeek(lexer.COLON) {
				return nil
			}
			decl.Effects = p.parseEffectList()

		case p.curTokenIs(lexer.FN):
			fn := p.parseFnDecl()
			decl.RunFn = fn
		}

		p.nextToken()
	}

	return decl
}

func (p *Parser) parseAgentDecl() *AgentDecl {
	decl := &AgentDecl{Token: p.curToken}

	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	decl.Name = p.curToken.Literal

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}

	decl.MaxTurns = 10
	decl.Temperature = 0.0

	p.nextToken()
	for !p.curTokenIs(lexer.RBRACE) && !p.curTokenIs(lexer.EOF) {
		switch {
		case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "model":
			if !p.expectPeek(lexer.COLON) {
				return nil
			}
			p.nextToken()
			if sl, ok := p.parseExpression(LOWEST).(*StringLiteral); ok {
				decl.Model = sl.Value
			}

		case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "system":
			if !p.expectPeek(lexer.COLON) {
				return nil
			}
			p.nextToken()
			if sl, ok := p.parseExpression(LOWEST).(*StringLiteral); ok {
				decl.System = sl.Value
			}

		case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "tools":
			if !p.expectPeek(lexer.COLON) {
				return nil
			}
			if !p.expectPeek(lexer.LBRACKET) {
				return nil
			}
			p.nextToken()
			for !p.curTokenIs(lexer.RBRACKET) && !p.curTokenIs(lexer.EOF) {
				if p.curTokenIs(lexer.IDENT) || p.curTokenIs(lexer.TOOL) {
					decl.Tools = append(decl.Tools, p.curToken.Literal)
				}
				p.nextToken()
				if p.curTokenIs(lexer.COMMA) {
					p.nextToken()
				}
			}

		case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "output":
			// Agents only support the structured block form.
			if !p.expectPeek(lexer.LBRACE) {
				return nil
			}
			decl.Outputs = p.parseFieldList()

		case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "config":
			if !p.expectPeek(lexer.LBRACE) {
				return nil
			}
			p.nextToken()
			for !p.curTokenIs(lexer.RBRACE) && !p.curTokenIs(lexer.EOF) {
				key := p.curToken.Literal
				if !p.expectPeek(lexer.COLON) {
					return nil
				}
				p.nextToken()
				valExpr := p.parseExpression(LOWEST)
				switch key {
				case "max_turns":
					if il, ok := valExpr.(*IntegerLiteral); ok {
						decl.MaxTurns = int(il.Value)
					}
				case "budget_tokens":
					if il, ok := valExpr.(*IntegerLiteral); ok {
						decl.BudgetTokens = int(il.Value)
					}
				case "retry_invalid_output":
					if il, ok := valExpr.(*IntegerLiteral); ok {
						decl.RetryInvalidOutput = int(il.Value)
					}
				case "temperature":
					if fl, ok := valExpr.(*FloatLiteral); ok {
						decl.Temperature = fl.Value
					} else if il, ok := valExpr.(*IntegerLiteral); ok {
						decl.Temperature = float64(il.Value)
					}
				}
				p.nextToken()
				if p.curTokenIs(lexer.COMMA) {
					p.nextToken()
				}
			}
		}

		p.nextToken()
	}

	return decl
}

func (p *Parser) parseMCPDecl() *MCPDecl {
	decl := &MCPDecl{Token: p.curToken}

	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	decl.Name = p.curToken.Literal

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}

	p.nextToken()
	for !p.curTokenIs(lexer.RBRACE) && !p.curTokenIs(lexer.EOF) {
		switch {
		case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "name":
			if !p.expectPeek(lexer.COLON) {
				return nil
			}
			p.nextToken()
			if sl, ok := p.parseExpression(LOWEST).(*StringLiteral); ok {
				decl.ServerName = sl.Value
			}

		case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "version":
			if !p.expectPeek(lexer.COLON) {
				return nil
			}
			p.nextToken()
			if sl, ok := p.parseExpression(LOWEST).(*StringLiteral); ok {
				decl.Version = sl.Value
			}

		case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "tools":
			if !p.expectPeek(lexer.COLON) {
				return nil
			}
			if !p.expectPeek(lexer.LBRACKET) {
				return nil
			}
			p.nextToken()
			for !p.curTokenIs(lexer.RBRACKET) && !p.curTokenIs(lexer.EOF) {
				if p.curTokenIs(lexer.IDENT) || p.curTokenIs(lexer.TOOL) {
					decl.Tools = append(decl.Tools, p.curToken.Literal)
				}
				p.nextToken()
				if p.curTokenIs(lexer.COMMA) {
					p.nextToken()
				}
			}

		case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "resources":
			if !p.expectPeek(lexer.COLON) {
				return nil
			}
			if !p.expectPeek(lexer.LBRACKET) {
				return nil
			}
			p.nextToken()
			for !p.curTokenIs(lexer.RBRACKET) && !p.curTokenIs(lexer.EOF) {
				// Each resource: Resource.static("name", uri: "uri", content: "...")
				if p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "Resource" {
					res := MCPResource{}
					// Skip Resource.static(
					p.nextToken() // .
					p.nextToken() // static
					p.nextToken() // (
					p.nextToken() // name string
					if sl, ok := p.parseExpression(LOWEST).(*StringLiteral); ok {
						res.Name = sl.Value
					}
					// Parse named args: uri: "...", content: "..."
					for !p.curTokenIs(lexer.RPAREN) && !p.curTokenIs(lexer.EOF) {
						if p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "uri" {
							p.nextToken() // :
							p.nextToken()
							if sl, ok := p.parseExpression(LOWEST).(*StringLiteral); ok {
								res.URI = sl.Value
							}
						}
						if p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "content" {
							p.nextToken() // :
							p.nextToken()
							if sl, ok := p.parseExpression(LOWEST).(*StringLiteral); ok {
								res.Content = sl.Value
							}
						}
						p.nextToken()
					}
					decl.Resources = append(decl.Resources, res)
				}
				p.nextToken()
				if p.curTokenIs(lexer.COMMA) {
					p.nextToken()
				}
			}

		case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "auth":
			if !p.expectPeek(lexer.COLON) {
				return nil
			}
			if !p.expectPeek(lexer.DOT) {
				return nil
			}
			p.nextToken()
			decl.Auth = p.curToken.Literal
			// Optional parameterised form: `.jwt(secret: "...")` — bare
			// `.api_key` leaves AuthArgs nil.
			if p.peekTokenIs(lexer.LPAREN) {
				p.nextToken()
				if !p.peekTokenIs(lexer.RPAREN) {
					for {
						p.nextToken()
						arg := CallArg{}
						if p.curTokenIs(lexer.IDENT) && p.peekTokenIs(lexer.COLON) {
							arg.Name = p.curToken.Literal
							p.nextToken()
							p.nextToken()
						}
						arg.Value = p.parseExpression(LOWEST)
						decl.AuthArgs = append(decl.AuthArgs, arg)
						if p.peekTokenIs(lexer.COMMA) {
							p.nextToken()
							continue
						}
						break
					}
				}
				if !p.expectPeek(lexer.RPAREN) {
					return nil
				}
			}

		case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "transport":
			if !p.expectPeek(lexer.COLON) {
				return nil
			}
			if !p.expectPeek(lexer.DOT) {
				return nil
			}
			p.nextToken()
			decl.Transport = p.curToken.Literal
		}

		p.nextToken()
	}

	return decl
}

func (p *Parser) parseServiceDecl() *ServiceDecl {
	decl := &ServiceDecl{Token: p.curToken, Port: 8080}

	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	decl.Name = p.curToken.Literal

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}

	p.nextToken()
	for !p.curTokenIs(lexer.RBRACE) && !p.curTokenIs(lexer.EOF) {
		switch {
		case p.curTokenIs(lexer.IDENT) && isHTTPMethod(p.curToken.Literal):
			route := ServiceRoute{Method: p.curToken.Literal}
			if !p.expectPeek(lexer.STRING_LIT) {
				return nil
			}
			route.Pattern = p.curToken.Literal
			if !p.expectPeek(lexer.ARROW) {
				return nil
			}
			if p.peekTokenIs(lexer.FN) {
				p.nextToken()
				fnDecl := &FnDecl{Token: p.curToken}
				fnDecl.Name = fmt.Sprintf("__inline_%s_%s__", route.Method, route.Pattern)
				if !p.expectPeek(lexer.LPAREN) {
					return nil
				}
				fnDecl.Params = p.parseFnParams()
				if p.peekTokenIs(lexer.ARROW) {
					p.nextToken()
					p.nextToken()
					fnDecl.ReturnType = p.parseTypeExpr()
				}
				if !p.expectPeek(lexer.LBRACE) {
					return nil
				}
				fnDecl.Body = p.parseBlockExpressionBody()
				route.InlineHandler = fnDecl
			} else {
				if !p.expectPeek(lexer.IDENT) {
					return nil
				}
				route.Handler = p.curToken.Literal
			}
			decl.Routes = append(decl.Routes, route)

		case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "prefix":
			if !p.expectPeek(lexer.COLON) {
				return nil
			}
			p.nextToken()
			if sl, ok := p.parseExpression(LOWEST).(*StringLiteral); ok {
				decl.Prefix = sl.Value
			}

		case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "middleware":
			if !p.expectPeek(lexer.COLON) {
				return nil
			}
			if !p.expectPeek(lexer.LBRACKET) {
				return nil
			}
			p.nextToken()
			for !p.curTokenIs(lexer.RBRACKET) && !p.curTokenIs(lexer.EOF) {
				if p.curTokenIs(lexer.IDENT) {
					ref := MiddlewareRef{Name: p.curToken.Literal}
					// Optional `.method(arg1, arg2, ...)` suffix.
					if p.peekTokenIs(lexer.DOT) {
						p.nextToken()
						if !p.expectPeek(lexer.IDENT) {
							return nil
						}
						ref.Method = p.curToken.Literal
						if p.peekTokenIs(lexer.LPAREN) {
							p.nextToken()
							// Empty-arg form: `Name.method()`.
							if p.peekTokenIs(lexer.RPAREN) {
								p.nextToken()
							} else {
								for {
									p.nextToken()
									arg := p.parseExpression(LOWEST)
									ref.Args = append(ref.Args, arg)
									if p.peekTokenIs(lexer.COMMA) {
										p.nextToken()
										continue
									}
									break
								}
								if !p.expectPeek(lexer.RPAREN) {
									return nil
								}
							}
						}
					}
					decl.Middlewares = append(decl.Middlewares, ref)
				}
				p.nextToken()
				if p.curTokenIs(lexer.COMMA) {
					p.nextToken()
				}
			}

		case p.curTokenIs(lexer.IDENT) && p.curToken.Literal == "transport":
			if !p.expectPeek(lexer.COLON) {
				return nil
			}
			if !p.expectPeek(lexer.DOT) {
				return nil
			}
			p.nextToken()
			// Expect .http(port: N) or .http(port: N, host: "...")
			if p.peekTokenIs(lexer.LPAREN) {
				p.nextToken()
				for !p.peekTokenIs(lexer.RPAREN) && !p.peekTokenIs(lexer.EOF) {
					p.nextToken()
					if !p.curTokenIs(lexer.IDENT) {
						break
					}
					argName := p.curToken.Literal
					if !p.expectPeek(lexer.COLON) {
						return nil
					}
					p.nextToken()
					val := p.parseExpression(LOWEST)
					switch argName {
					case "port":
						if il, ok := val.(*IntegerLiteral); ok {
							decl.Port = int(il.Value)
						}
					case "host":
						if sl, ok := val.(*StringLiteral); ok {
							decl.Host = sl.Value
						}
					}
					if p.peekTokenIs(lexer.COMMA) {
						p.nextToken()
					}
				}
				if !p.expectPeek(lexer.RPAREN) {
					return nil
				}
			}
		}

		p.nextToken()
	}

	return decl
}

func isHTTPMethod(s string) bool {
	switch s {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return true
	}
	return false
}

func (p *Parser) parseProtocolDecl() *ProtocolDecl {
	decl := &ProtocolDecl{Token: p.curToken}

	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	decl.Name = p.curToken.Literal

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}

	p.nextToken()
	for !p.curTokenIs(lexer.RBRACE) && !p.curTokenIs(lexer.EOF) {
		if p.curTokenIs(lexer.FN) {
			method := p.parseProtocolMethod()
			decl.Methods = append(decl.Methods, method)
		}
		p.nextToken()
	}

	return decl
}

func (p *Parser) parseProtocolMethod() ProtocolMethod {
	pm := ProtocolMethod{}
	p.nextToken()
	pm.Name = p.curToken.Literal

	if !p.expectPeek(lexer.LPAREN) {
		return pm
	}
	pm.Params = p.parseFnParams()

	if p.peekTokenIs(lexer.ARROW) {
		p.nextToken()
		p.nextToken()
		pm.ReturnType = p.parseTypeExpr()
	}

	if p.peekTokenIs(lexer.EFFECT) {
		p.nextToken()
		pm.Effects = p.parseEffectList()
	}

	return pm
}

func (p *Parser) parseImplDecl() *ImplDecl {
	decl := &ImplDecl{Token: p.curToken}

	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	firstIdent := p.curToken.Literal

	// `impl Type { ... }` (inherent) vs `impl Protocol for Type { ... }`.
	if p.peekTokenIs(lexer.FOR) {
		decl.Protocol = firstIdent
		p.nextToken()
		if !p.expectPeek(lexer.IDENT) {
			return nil
		}
		decl.Target = p.curToken.Literal
	} else {
		decl.Target = firstIdent
	}

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}

	p.nextToken()
	for !p.curTokenIs(lexer.RBRACE) && !p.curTokenIs(lexer.EOF) {
		if p.curTokenIs(lexer.FN) {
			fn := p.parseFnDecl()
			decl.Methods = append(decl.Methods, fn)
		}
		p.nextToken()
	}

	return decl
}

func (p *Parser) parseEffectDecl() *EffectDecl {
	decl := &EffectDecl{Token: p.curToken}

	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	decl.Name = p.curToken.Literal

	if !p.expectPeek(lexer.LBRACE) {
		return nil
	}

	p.nextToken()
	for !p.curTokenIs(lexer.RBRACE) && !p.curTokenIs(lexer.EOF) {
		if p.curTokenIs(lexer.IDENT) {
			op := EffectOperation{Name: p.curToken.Literal}
			if p.peekTokenIs(lexer.LPAREN) {
				p.nextToken()
				op.Params = p.parseFnParams()
			}
			if p.peekTokenIs(lexer.ARROW) {
				p.nextToken()
				p.nextToken()
				op.ReturnType = p.parseTypeExpr()
			}
			decl.Operations = append(decl.Operations, op)
		}
		p.nextToken()
	}

	return decl
}

func (p *Parser) parseTypeAliasDecl() *TypeAliasDecl {
	decl := &TypeAliasDecl{Token: p.curToken}

	if !p.expectPeek(lexer.IDENT) {
		return nil
	}
	decl.Name = p.curToken.Literal

	if !p.expectPeek(lexer.ASSIGN) {
		return nil
	}
	p.nextToken()
	decl.TypeExpr = p.parseTypeExpr()

	return decl
}

// parseImportStatement parses:
//
//	import "./path"
//	import "./path" as Name
//	import { a, b } from "./path"
func (p *Parser) parseImportStatement() *ImportStatement {
	stmt := &ImportStatement{Token: p.curToken}

	// Check for selective import: import { a, b } from "./path"
	if p.peekTokenIs(lexer.LBRACE) {
		p.nextToken()
		p.nextToken()
		for !p.curTokenIs(lexer.RBRACE) && !p.curTokenIs(lexer.EOF) {
			stmt.Names = append(stmt.Names, p.curToken.Literal)
			if p.peekTokenIs(lexer.COMMA) {
				p.nextToken()
			}
			p.nextToken()
		}
		// Expect 'from' (contextual identifier)
		if !p.expectPeek(lexer.IDENT) || p.curToken.Literal != "from" {
			p.errors = append(p.errors, fmt.Sprintf("expected 'from' after import { ... }, got %s", p.curToken.Literal))
			return nil
		}
		if !p.expectPeek(lexer.STRING_LIT) {
			return nil
		}
		stmt.Path = p.curToken.Literal
		return stmt
	}

	// Simple or aliased import: import "./path" [as Name]
	if !p.expectPeek(lexer.STRING_LIT) {
		return nil
	}
	stmt.Path = p.curToken.Literal

	// Optional: as Name
	if p.peekTokenIs(lexer.IDENT) && p.peekToken.Literal == "as" {
		p.nextToken()
		if !p.expectPeek(lexer.IDENT) {
			return nil
		}
		stmt.Alias = p.curToken.Literal
	}

	return stmt
}

// parseTypeExpr serializes a type expression to a string, handling generics
// (Option[String]), nesting (Map[String, List[Int]]), and list shorthand ([Int]).
func (p *Parser) parseTypeExpr() string {
	// List shorthand: [Type]
	if p.curTokenIs(lexer.LBRACKET) {
		result := "["
		p.nextToken()
		result += p.parseTypeExpr()
		if p.peekTokenIs(lexer.RBRACKET) {
			p.nextToken()
		}
		result += "]"
		return result
	}

	var result strings.Builder
	result.WriteString(p.curToken.Literal)

	// Dotted type path: Module.Type
	for p.peekTokenIs(lexer.DOT) {
		p.nextToken()
		p.nextToken()
		result.WriteString("." + p.curToken.Literal)
	}

	// Check for generic: Name[T, U]
	if p.peekTokenIs(lexer.LBRACKET) {
		p.nextToken()
		result.WriteString("[")
		p.nextToken()
		result.WriteString(p.parseTypeExpr())

		for p.peekTokenIs(lexer.COMMA) {
			p.nextToken()
			result.WriteString(", ")
			p.nextToken()
			result.WriteString(p.parseTypeExpr())
		}

		if p.peekTokenIs(lexer.RBRACKET) {
			p.nextToken()
		}
		result.WriteString("]")
	}

	return result.String()
}

func (p *Parser) isCapability(t lexer.TokenType) bool {
	switch t {
	case lexer.ISO, lexer.TRN, lexer.REF, lexer.VAL, lexer.BOX, lexer.TAG:
		return true
	}
	return false
}

func (p *Parser) parseExpressionList(end lexer.TokenType) []Expression {
	var list []Expression

	if p.peekTokenIs(end) {
		p.nextToken()
		return list
	}

	p.nextToken()
	list = append(list, p.parseExpression(LOWEST))

	for p.peekTokenIs(lexer.COMMA) {
		p.nextToken()
		if p.peekTokenIs(end) {
			break
		}
		p.nextToken()
		list = append(list, p.parseExpression(LOWEST))
	}

	if !p.expectPeek(end) {
		return nil
	}

	return list
}

// parseSpreadExpression handles ...expr (e.g. inside list literals).
func (p *Parser) parseSpreadExpression() Expression {
	tok := p.curToken
	p.nextToken()
	value := p.parseExpression(PREFIX)
	return &SpreadExpression{Token: tok, Value: value}
}

func (p *Parser) parseSuperExpression() Expression {
	return &SuperExpression{Token: p.curToken}
}

// parseLetDestructure handles `let [a, b, ...rest] = expr`.
func (p *Parser) parseLetDestructure(letToken lexer.Token) *LetDestructureStatement {
	stmt := &LetDestructureStatement{Token: letToken}
	stmt.Pattern.Kind = "list"

	p.nextToken()
	p.nextToken()

	idx := 0
	for !p.curTokenIs(lexer.RBRACKET) && !p.curTokenIs(lexer.EOF) {
		if p.curTokenIs(lexer.SPREAD) {
			// ...rest
			p.nextToken()
			stmt.Pattern.RestName = p.curToken.Literal
			stmt.Pattern.RestIdx = idx
		} else {
			stmt.Pattern.Names = append(stmt.Pattern.Names, p.curToken.Literal)
			idx++
		}

		if p.peekTokenIs(lexer.COMMA) {
			p.nextToken()
		}
		p.nextToken()
	}

	// curToken is now ']'
	if !p.expectPeek(lexer.ASSIGN) {
		return nil
	}
	p.nextToken()
	stmt.Value = p.parseExpression(LOWEST)

	if p.peekTokenIs(lexer.SEMICOLON) {
		p.nextToken()
	}

	return stmt
}

// parseExportStatement handles `export <decl>`.
func (p *Parser) parseExportStatement() *ExportStatement {
	tok := p.curToken
	p.nextToken()
	inner := p.parseStatement()
	return &ExportStatement{Token: tok, Inner: inner}
}

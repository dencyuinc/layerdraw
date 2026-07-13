// SPDX-License-Identifier: LicenseRef-LayerDraw-1.0

package syntax

// ParseResult is the complete syntax frontend output.
type ParseResult struct {
	Root        *Node
	Tokens      []Token
	Diagnostics []Diagnostic
}

// Parse lexes and parses LDL source into a lossless CST.
func Parse(src []byte) ParseResult {
	lexed := Lex(src)
	p := parser{tokens: lexed.Tokens, diagnostics: append([]Diagnostic{}, lexed.Diagnostics...)}
	root := p.parseFile()
	root.Span = Span{Start: 0, End: len(src)}
	sortDiagnostics(p.diagnostics)
	return ParseResult{Root: root, Tokens: lexed.Tokens, Diagnostics: p.diagnostics}
}

type parser struct {
	tokens      []Token
	pos         int
	diagnostics []Diagnostic
}

func (p *parser) parseFile() *Node {
	file := newNode(NodeFile)
	sawContent := false
	sawDeclaration := false
	for !p.at(TokenEOF) {
		switch {
		case p.at(TokenModuleDoc):
			if sawContent {
				p.diagnostics = append(p.diagnostics, invalidStructure(p.peek().Span, "module documentation must appear before imports and declarations"))
			}
			file.append(p.parseTriviaToken())
		case p.at(TokenNewline) || p.at(TokenLineComment):
			file.append(p.parseTriviaToken())
		case p.atKeyword("import"):
			if sawDeclaration {
				p.diagnostics = append(p.diagnostics, invalidStructure(p.peek().Span, "imports must appear before declarations"))
			}
			file.append(p.parseImportDecl())
			sawContent = true
		case p.at(TokenDocComment):
			file.append(p.parseTriviaToken())
		case p.atDeclarationStart():
			file.append(p.parseDeclaration())
			sawContent = true
			sawDeclaration = true
		default:
			file.append(p.errorNodeUntil("expected import or declaration", p.isTopLevelBoundary))
		}
	}
	file.append(p.consume())
	return file
}

func (p *parser) parseImportDecl() *Node {
	n := newNode(NodeImportDecl, p.expectKeyword("import"))
	if p.at(TokenLBrace) {
		n.append(p.consume(), p.parseImportItems())
		p.expectInto(n, TokenRBrace)
	} else {
		p.expectInto(n, TokenIdentifier)
	}
	p.expectKeywordInto(n, "from")
	p.expectInto(n, TokenString)
	p.consumeLineEnd(n)
	return n
}

func (p *parser) parseImportItems() *Node {
	return p.parseDelimited(NodeImportItems, NodeImportItem, TokenRBrace, true, func(item *Node) {
		p.expectInto(item, TokenIdentifier)
		if p.atKeyword("as") {
			item.append(p.consume())
			p.expectInto(item, TokenIdentifier)
		}
	})
}

func (p *parser) parseDeclaration() *Node {
	n := newNode(NodeDeclaration)
	switch {
	case p.atKeyword("project"):
		n.append(p.consume(), p.expect(TokenIdentifier), p.expect(TokenString), p.parseBlock())
	case p.atKeyword("layers"):
		n.append(p.consume(), p.parseItemBlock("layer"))
	case p.atKeyword("entity_type"):
		n.append(p.consume(), p.expect(TokenIdentifier), p.expect(TokenString), p.parseBlock())
	case p.atKeyword("relation_type"):
		n.append(p.consume(), p.expect(TokenIdentifier), p.expect(TokenString), p.expect(TokenIdentifier), p.parseBlock())
	case p.atKeyword("entities"):
		n.append(p.consume(), p.parseSymbolRef(), p.expect(TokenAt), p.parseSymbolRef(), p.parseItemBlock("entity"))
	case p.atKeyword("rows"):
		n.append(p.consume(), p.parseSymbolRef(), p.parseColumnHeader(), p.parseItemBlock("row"))
	case p.atKeyword("relations"):
		n.append(p.consume(), p.parseSymbolRef(), p.parseItemBlock("relation"))
	case p.atKeyword("relation_rows"):
		n.append(p.consume(), p.parseSymbolRef(), p.parseColumnHeader(), p.parseItemBlock("row"))
	case p.atKeyword("query"):
		n.append(p.consume(), p.expect(TokenIdentifier), p.expect(TokenString), p.parseBlock())
	case p.atKeyword("view"):
		n.append(p.consume(), p.expect(TokenIdentifier), p.expect(TokenString), p.expect(TokenIdentifier), p.parseBlock())
	case p.atKeyword("reference"):
		n.append(p.consume(), p.expect(TokenIdentifier), p.expect(TokenHeredoc))
		p.consumeLineEnd(n)
	case p.atKeyword("reserved"):
		n.append(p.consume(), p.parseBlock())
	case p.atKeyword("moves"):
		n.append(p.consume(), p.parseItemBlock("move"))
	case p.atKeyword("export"):
		n.append(p.parseExportDecl())
	default:
		n.append(p.errorNode("unknown declaration"))
	}
	return n
}

func (p *parser) parseExportDecl() *Node {
	n := newNode(NodeExportDecl, p.expectKeyword("export"))
	if p.at(TokenStar) {
		n.append(p.consume())
		p.expectKeywordInto(n, "from")
		p.expectInto(n, TokenString)
		p.consumeLineEnd(n)
		return n
	}
	p.expectInto(n, TokenLBrace)
	n.append(p.parseExportItems())
	p.expectInto(n, TokenRBrace)
	if p.atKeyword("from") {
		n.append(p.consume())
		p.expectInto(n, TokenString)
	}
	p.consumeLineEnd(n)
	return n
}

func (p *parser) parseExportItems() *Node {
	return p.parseDelimited(NodeExportItems, NodeExportItem, TokenRBrace, true, func(item *Node) {
		p.expectInto(item, TokenIdentifier)
		if p.atKeyword("as") {
			item.append(p.consume())
			p.expectInto(item, TokenIdentifier)
		}
	})
}

func (p *parser) parseItemBlock(item string) *Node {
	n := newNode(NodeItemBlock)
	p.expectInto(n, TokenLBrace)
	if p.at(TokenRBrace) {
		n.append(p.consume())
		return n
	}
	p.expectNewlineInto(n)
	for !p.at(TokenEOF) && !p.at(TokenRBrace) {
		if p.atTriviaToken() || p.at(TokenDocComment) {
			if p.at(TokenModuleDoc) {
				p.diagnostics = append(p.diagnostics, invalidStructure(p.peek().Span, "module documentation is only valid at the start of a file"))
			}
			n.append(p.parseTriviaToken())
			continue
		}
		switch item {
		case "layer":
			n.append(p.parseLayerItem())
		case "entity":
			n.append(p.parseEntityItem())
		case "relation":
			n.append(p.parseRelationItem())
		case "row":
			n.append(p.parseRowItem())
		case "move":
			n.append(p.parseMoveItem())
		}
		p.expectLineEndInto(n)
	}
	p.expectInto(n, TokenRBrace)
	return n
}

func (p *parser) parseLayerItem() *Node {
	n := newNode(NodeLayerItem, p.expect(TokenIdentifier), p.expect(TokenString), p.expect(TokenAt), p.expect(TokenInteger))
	if p.at(TokenLBrace) {
		n.append(p.parseBlock())
	}
	return n
}

func (p *parser) parseEntityItem() *Node {
	n := newNode(NodeEntityItem, p.expect(TokenIdentifier), p.expect(TokenString))
	if p.at(TokenLBrace) {
		n.append(p.parseBlock())
	}
	return n
}

func (p *parser) parseRelationItem() *Node {
	n := newNode(NodeRelationItem, p.expect(TokenIdentifier), p.expect(TokenColon), p.parseSymbolRef(), p.expect(TokenArrow), p.parseSymbolRef())
	if p.at(TokenString) {
		n.append(p.consume())
	}
	if p.at(TokenLBrace) {
		n.append(p.parseBlock())
	}
	return n
}

func (p *parser) parseRowItem() *Node {
	n := newNode(NodeRowItem, p.expect(TokenIdentifier), p.expect(TokenIdentifier), p.expect(TokenColon), p.parseCells())
	return n
}

func (p *parser) parseMoveItem() *Node {
	n := newNode(NodeMoveItem, p.expect(TokenIdentifier), p.expect(TokenIdentifier))
	if !p.at(TokenArrow) {
		n.append(p.expect(TokenIdentifier))
	}
	n.append(p.expect(TokenArrow), p.expect(TokenIdentifier))
	return n
}

func (p *parser) parseColumnHeader() *Node {
	n := newNode(NodeColumnHeader)
	p.expectInto(n, TokenLBracket)
	if p.at(TokenRBracket) {
		n.append(p.errorAtCurrent("expected column header item"))
	}
	for !p.at(TokenEOF) && !p.at(TokenRBracket) {
		if p.atTriviaToken() {
			n.append(p.parseTriviaToken())
			continue
		}
		n.append(p.parseSymbolRef())
		if p.at(TokenComma) {
			n.append(p.consume())
		} else {
			break
		}
	}
	p.expectInto(n, TokenRBracket)
	return n
}

func (p *parser) parseCells() *Node {
	n := newNode(NodeCells)
	sawCell := false
	for !p.at(TokenEOF) && !p.at(TokenNewline) && !p.at(TokenRBrace) {
		if p.at(TokenUnderscore) {
			n.append(p.consume())
		} else {
			n.append(p.parseValue())
		}
		sawCell = true
		if p.at(TokenComma) {
			n.append(p.consume())
			if p.at(TokenNewline) || p.at(TokenRBrace) || p.at(TokenEOF) {
				n.append(p.errorAtCurrent("expected cell after comma"))
				break
			}
		} else {
			break
		}
	}
	if !sawCell {
		n.append(p.errorAtCurrent("expected row cell"))
	}
	return n
}

func (p *parser) parseBlock() *Node {
	n := newNode(NodeBlock)
	p.expectInto(n, TokenLBrace)
	if p.at(TokenRBrace) {
		n.append(p.consume())
		return n
	}
	p.expectNewlineInto(n)
	for !p.at(TokenEOF) && !p.at(TokenRBrace) {
		if p.atTriviaToken() || p.at(TokenDocComment) {
			if p.at(TokenModuleDoc) {
				p.diagnostics = append(p.diagnostics, invalidStructure(p.peek().Span, "module documentation is only valid at the start of a file"))
			}
			n.append(p.parseTriviaToken())
			continue
		}
		if p.at(TokenIdentifier) {
			n.append(p.parseStatementOrNestedBlock())
			p.expectLineEndInto(n)
			continue
		}
		n.append(p.errorNodeUntil("expected statement or nested block", p.isLineOrBlockBoundary))
	}
	p.expectInto(n, TokenRBrace)
	return n
}

func (p *parser) parseStatementOrNestedBlock() *Node {
	n := newNode(NodeStatement, p.expect(TokenIdentifier))
	for !p.at(TokenEOF) && !p.at(TokenNewline) && !p.at(TokenRBrace) {
		if p.at(TokenLBrace) && !p.looksObject() {
			break
		}
		n.append(p.parseStatementArg())
	}
	if p.at(TokenLBrace) {
		nb := newNode(NodeNestedBlock, n.Children...)
		nb.append(p.parseBlock())
		return nb
	}
	return n
}

func (p *parser) looksObject() bool {
	if !p.at(TokenLBrace) {
		return false
	}
	if p.look(1).Kind == TokenRBrace {
		return true
	}
	return (p.look(1).Kind == TokenIdentifier || p.look(1).Kind == TokenString) && p.look(2).Kind == TokenColon
}

func (p *parser) parseStatementArg() Element {
	switch {
	case p.at(TokenLBracket):
		return p.parseColumnHeader()
	case isValueStart(p.peek().Kind):
		return p.parseValue()
	case p.at(TokenEqualEqual) || p.at(TokenBangEqual) || p.at(TokenLess) || p.at(TokenLessEqual) || p.at(TokenGreater) || p.at(TokenGreaterEqual) || p.at(TokenArrow):
		return p.consume()
	default:
		return p.errorNode("expected statement argument")
	}
}

func (p *parser) parseValue() *Node {
	n := newNode(NodeValue)
	switch p.peek().Kind {
	case TokenString, TokenHeredoc, TokenNumber:
		n.append(p.consume())
	case TokenInteger:
		if p.look(1).Kind == TokenDotDot {
			n.append(p.parseRange())
		} else {
			n.append(p.consume())
		}
	case TokenIdentifier:
		n.append(p.parseQualifiedToken())
	case TokenDollar:
		n.append(p.parseParameterRef())
	case TokenLBracket:
		n.append(p.parseList())
	case TokenLBrace:
		n.append(p.parseObject())
	default:
		n.append(p.errorNode("expected value"))
	}
	n.refreshSpan()
	return n
}

func (p *parser) parseRange() *Node {
	n := newNode(NodeRange, p.expect(TokenInteger), p.expect(TokenDotDot))
	if p.at(TokenInteger) || p.at(TokenStar) {
		n.append(p.consume())
	} else {
		n.append(p.errorNode("expected range upper bound"))
	}
	return n
}

func (p *parser) parseParameterRef() *Node {
	return newNode(NodeParameterRef, p.expect(TokenDollar), p.expect(TokenIdentifier))
}

func (p *parser) parseSymbolRef() *Node {
	n := newNode(NodeSymbolRef, p.expect(TokenIdentifier))
	if p.at(TokenDot) {
		n.append(p.consume(), p.expect(TokenIdentifier))
	}
	return n
}

func (p *parser) parseQualifiedToken() *Node {
	n := newNode(NodeQualifiedToken, p.expect(TokenIdentifier))
	for p.at(TokenDot) {
		n.append(p.consume(), p.expect(TokenIdentifier))
	}
	return n
}

func (p *parser) parseList() *Node {
	n := newNode(NodeList)
	multiline := false
	trailingComma := false
	p.expectInto(n, TokenLBracket)
	for !p.at(TokenEOF) && !p.at(TokenRBracket) {
		if p.atTriviaToken() {
			if p.at(TokenNewline) {
				multiline = true
			}
			n.append(p.parseTriviaToken())
			continue
		}
		n.append(p.parseValue())
		if p.at(TokenComma) {
			n.append(p.consume())
			trailingComma = true
		} else {
			trailingComma = false
			break
		}
	}
	if trailingComma && !multiline {
		n.append(p.errorAtCurrent("trailing comma requires multiline list"))
	}
	p.expectInto(n, TokenRBracket)
	return n
}

func (p *parser) parseObject() *Node {
	n := newNode(NodeObject)
	multiline := false
	trailingComma := false
	p.expectInto(n, TokenLBrace)
	for !p.at(TokenEOF) && !p.at(TokenRBrace) {
		if p.atTriviaToken() {
			if p.at(TokenNewline) {
				multiline = true
			}
			n.append(p.parseTriviaToken())
			continue
		}
		item := newNode(NodeObjectItem)
		if p.at(TokenIdentifier) || p.at(TokenString) {
			item.append(p.consume())
		} else {
			item.append(p.errorNode("expected object key"))
		}
		p.expectInto(item, TokenColon)
		item.append(p.parseValue())
		n.append(item)
		if p.at(TokenComma) {
			n.append(p.consume())
			trailingComma = true
		} else {
			trailingComma = false
			break
		}
	}
	if trailingComma && !multiline {
		n.append(p.errorAtCurrent("trailing comma requires multiline object"))
	}
	p.expectInto(n, TokenRBrace)
	return n
}

func (p *parser) parseDelimited(listKind NodeKind, itemKind NodeKind, stop TokenKind, requireNonEmpty bool, parseItem func(*Node)) *Node {
	list := newNode(listKind)
	if requireNonEmpty && p.at(stop) {
		list.append(p.errorAtCurrent("expected list item"))
		return list
	}
	for !p.at(TokenEOF) && !p.at(stop) {
		if p.atTriviaToken() {
			list.append(p.parseTriviaToken())
			continue
		}
		item := newNode(itemKind)
		parseItem(item)
		list.append(item)
		if p.at(TokenComma) {
			list.append(p.consume())
		} else {
			break
		}
	}
	return list
}

func (p *parser) parseTriviaToken() *Node {
	return newNode(NodeComment, p.consume())
}

func (p *parser) consumeLineEnd(n *Node) {
	if p.at(TokenNewline) {
		n.append(p.consume())
	}
}

func (p *parser) expectNewlineInto(n *Node) {
	if p.at(TokenNewline) {
		n.append(p.consume())
		return
	}
	n.append(p.errorAtCurrent("expected newline"))
}

func (p *parser) expectLineEndInto(n *Node) {
	if p.at(TokenNewline) {
		n.append(p.consume())
		return
	}
	if p.at(TokenLineComment) {
		n.append(p.consume())
		if p.at(TokenNewline) {
			n.append(p.consume())
		}
		return
	}
	if p.at(TokenRBrace) || p.at(TokenEOF) {
		return
	}
	n.append(p.errorNodeUntil("expected line terminator", p.isLineOrBlockBoundary))
	if p.at(TokenNewline) {
		n.append(p.consume())
	}
}

func (p *parser) expectInto(n *Node, kind TokenKind) {
	n.append(p.expect(kind))
}

func (p *parser) expectKeywordInto(n *Node, keyword string) {
	n.append(p.expectKeyword(keyword))
}

func (p *parser) expect(kind TokenKind) Element {
	if p.at(kind) {
		return p.consume()
	}
	return p.errorNode("expected " + kind.String())
}

func (p *parser) expectKeyword(keyword string) Element {
	if p.atKeyword(keyword) {
		return p.consume()
	}
	return p.errorNode("expected " + keyword)
}

func (p *parser) consume() Element {
	if p.pos >= len(p.tokens) {
		return TokenElement{Index: len(p.tokens) - 1, Token: p.tokens[len(p.tokens)-1]}
	}
	tok := p.tokens[p.pos]
	el := TokenElement{Index: p.pos, Token: tok}
	p.pos++
	return el
}

func (p *parser) errorNode(message string) *Node {
	tok := p.peek()
	span := tok.Span
	p.diagnostics = append(p.diagnostics, invalidStructure(span, message))
	if tok.Kind == TokenEOF {
		return &Node{Kind: NodeError, Span: span}
	}
	return newNode(NodeError, p.consume())
}

func (p *parser) errorAtCurrent(message string) *Node {
	tok := p.peek()
	p.diagnostics = append(p.diagnostics, invalidStructure(tok.Span, message))
	return &Node{Kind: NodeError, Span: tok.Span}
}

func (p *parser) errorNodeUntil(message string, stop func() bool) *Node {
	start := p.peek().Span
	p.diagnostics = append(p.diagnostics, invalidStructure(start, message))
	n := &Node{Kind: NodeError, Span: start}
	for !p.at(TokenEOF) && !stop() {
		n.append(p.consume())
	}
	return n
}

func (p *parser) isTopLevelBoundary() bool {
	return p.atDeclarationStart() || p.atKeyword("import")
}

func (p *parser) isLineOrBlockBoundary() bool {
	return p.at(TokenNewline) || p.at(TokenRBrace)
}

func (p *parser) at(kind TokenKind) bool {
	return p.peek().Kind == kind
}

func (p *parser) atKeyword(keyword string) bool {
	return isKeyword(p.peek(), keyword)
}

func (p *parser) atTriviaToken() bool {
	return p.at(TokenNewline) || p.at(TokenLineComment) || p.at(TokenModuleDoc)
}

func (p *parser) atDeclarationStart() bool {
	if p.peek().Kind != TokenIdentifier {
		return false
	}
	switch p.peek().Raw {
	case "project", "layers", "entity_type", "relation_type", "entities", "rows",
		"relations", "relation_rows", "query", "view", "reference", "reserved",
		"moves", "export":
		return true
	default:
		return false
	}
}

func (p *parser) peek() Token {
	return p.look(0)
}

func (p *parser) look(offset int) Token {
	idx := p.pos + offset
	if idx >= len(p.tokens) {
		return p.tokens[len(p.tokens)-1]
	}
	return p.tokens[idx]
}

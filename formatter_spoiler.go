package main

import (
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"maunium.net/go/mautrix/format/mdext"
)

type discordSpoilerParser struct {
	parser.InlineParser
}

func (s *discordSpoilerParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, segment := block.PeekLine()
	node := s.InlineParser.Parse(parent, block, pc)
	delimiter, ok := node.(*parser.Delimiter)
	if !ok || delimiter.OriginalLength != 2 {
		return node
	}
	// Discord permits a single inner space where Markdown's flanking rules don't.
	if len(line) > 3 && line[2] == ' ' && !util.IsSpaceRune(util.ToRune(line, 3)) {
		delimiter.CanOpen = true
	}
	source := block.Source()
	if segment.Start > 1 && source[segment.Start-1] == ' ' {
		beforeSpace, _ := utf8.DecodeLastRune(source[:segment.Start-1])
		if !util.IsSpaceRune(beforeSpace) {
			delimiter.CanClose = true
		}
	}
	return node
}

type discordSpoilerExtension struct{}

func (discordSpoilerExtension) Extend(m goldmark.Markdown) {
	mdext.SimpleSpoiler.Extend(m)
	m.Parser().AddOptions(parser.WithInlineParsers(util.Prioritized(
		&discordSpoilerParser{InlineParser: mdext.NewSimpleSpoilerParser()}, 499,
	)))
}

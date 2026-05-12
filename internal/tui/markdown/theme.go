package markdown

import "charm.land/glamour/v2/ansi"

// DarkStyleConfig is the default dark style.
var CustomDarkStyleConfig = ansi.StyleConfig{
	Document: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockPrefix: "",
			BlockSuffix: "",
			Prefix:      "",
		},
		Margin:      uintPtr(0),
		IndentToken: stringPtr(""),
		Indent:      uintPtr(0),
	},
	Paragraph: ansi.StyleBlock{
		Margin:      uintPtr(0),
		IndentToken: stringPtr(""),
		Indent:      uintPtr(0),
		StylePrimitive: ansi.StylePrimitive{
			BlockPrefix: "",
			BlockSuffix: "",
			Prefix:      "",
		},
	},
	BlockQuote: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{},
		Indent:         uintPtr(1),
		IndentToken:    stringPtr("│ "),
	},
	List: ansi.StyleList{
		LevelIndent: 2,
	},
	Heading: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			BlockSuffix: "",
			Color:       stringPtr("#666CA6"),
			Bold:        boolPtr(true),
		},
	},
	H1: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Prefix: "# ",
			Color:  stringPtr("255"),
			Bold:   boolPtr(true),
		},
	},
	H2: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Color:  stringPtr("252"),
			Prefix: "## ",
		},
	},
	H3: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Color:  stringPtr("251"),
			Prefix: "### ",
		},
	},
	H4: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Color:  stringPtr("250"),
			Prefix: "#### ",
		},
	},
	H5: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Color:  stringPtr("249"),
			Prefix: "##### ",
		},
	},
	H6: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Prefix: "###### ",
			Color:  stringPtr("248"),
			Bold:   boolPtr(false),
		},
	},
	Strikethrough: ansi.StylePrimitive{
		CrossedOut: boolPtr(true),
	},
	Emph: ansi.StylePrimitive{
		Italic: boolPtr(true),
	},
	Strong: ansi.StylePrimitive{
		Bold: boolPtr(true),
	},
	HorizontalRule: ansi.StylePrimitive{
		Color:  stringPtr("240"),
		Format: "\n--------\n",
	},
	Item: ansi.StylePrimitive{
		BlockPrefix: "• ",
	},
	Enumeration: ansi.StylePrimitive{
		BlockPrefix: ". ",
	},
	Task: ansi.StyleTask{
		StylePrimitive: ansi.StylePrimitive{},
		Ticked:         "[✓] ",
		Unticked:       "[ ] ",
	},
	Link: ansi.StylePrimitive{
		Color:       stringPtr("#666CA6"),
		Underline:   boolPtr(false),
		BlockPrefix: "",
		BlockSuffix: "",
		Format:      "",
	},
	LinkText: ansi.StylePrimitive{
		Color: stringPtr("#666CA6"),
		Bold:  boolPtr(true),
	},
	Image: ansi.StylePrimitive{
		Underline: boolPtr(false),
		Color:     stringPtr("#666CA6"),
		Bold:      boolPtr(false),
		Format:    "",
	},
	ImageText: ansi.StylePrimitive{
		Color:  stringPtr("#666CA6"),
		Bold:   boolPtr(true),
		Format: "",
	},
	Code: ansi.StyleBlock{
		StylePrimitive: ansi.StylePrimitive{
			Color:       stringPtr("#E2E1ED"),
			Prefix:      "`",
			Suffix:      "`",
			BlockPrefix: "",
			BlockSuffix: "",
		},
	},
	CodeBlock: ansi.StyleCodeBlock{
		StyleBlock: ansi.StyleBlock{
			// Indent and bg are handled in padCodeBlockLines (the
			// post-render pass) because glamour's IndentWriter styles
			// indents with the *parent* block's StylePrimitive (which
			// has no bg), and chroma's TTY formatters strip global
			// background. Leaving Indent at 0 here means glamour
			// writes code lines flush-left; the post-processor then
			// prepends a 2-col bg stripe and pads each line out to
			// the wrap width with the same bg — yielding one
			// continuous framed region per fenced block.
			StylePrimitive: ansi.StylePrimitive{
				Color: stringPtr("244"),
			},
			Margin: uintPtr(0),
		},
		// Code-block syntax highlighting palette: GitHub PrettyLights
		// "light" (the same theme github.com renders by default).
		// Pairs with the #F6F8FA bg in codepad.go so a code fence in
		// a PR body looks like the same fence on github.com — dark
		// navy/red/blue tokens on a light background. The previous
		// dark-on-dark palette was iterated through eight bg shades
		// before the user noted they wanted to match GitHub directly.
		Chroma: &ansi.Chroma{
			Text: ansi.StylePrimitive{
				Color: stringPtr("#1F2328"),
			},
			Error: ansi.StylePrimitive{
				Color:           stringPtr("#F6F8FA"),
				BackgroundColor: stringPtr("#82071E"),
			},
			Comment: ansi.StylePrimitive{
				Color: stringPtr("#6E7781"),
			},
			CommentPreproc: ansi.StylePrimitive{
				Color: stringPtr("#CF222E"),
			},
			Keyword: ansi.StylePrimitive{
				Color: stringPtr("#CF222E"),
			},
			KeywordReserved: ansi.StylePrimitive{
				Color: stringPtr("#CF222E"),
			},
			KeywordNamespace: ansi.StylePrimitive{
				Color: stringPtr("#CF222E"),
			},
			KeywordType: ansi.StylePrimitive{
				Color: stringPtr("#953800"),
			},
			Operator: ansi.StylePrimitive{
				Color: stringPtr("#CF222E"),
			},
			Punctuation: ansi.StylePrimitive{
				Color: stringPtr("#1F2328"),
			},
			Name: ansi.StylePrimitive{
				Color: stringPtr("#1F2328"),
			},
			NameBuiltin: ansi.StylePrimitive{
				Color: stringPtr("#0550AE"),
			},
			NameTag: ansi.StylePrimitive{
				Color: stringPtr("#116329"),
			},
			NameAttribute: ansi.StylePrimitive{
				Color: stringPtr("#0550AE"),
			},
			NameClass: ansi.StylePrimitive{
				Color: stringPtr("#953800"),
				Bold:  boolPtr(true),
			},
			NameDecorator: ansi.StylePrimitive{
				Color: stringPtr("#0550AE"),
			},
			NameFunction: ansi.StylePrimitive{
				Color: stringPtr("#8250DF"),
			},
			LiteralNumber: ansi.StylePrimitive{
				Color: stringPtr("#0550AE"),
			},
			LiteralString: ansi.StylePrimitive{
				Color: stringPtr("#0A3069"),
			},
			LiteralStringEscape: ansi.StylePrimitive{
				Color: stringPtr("#0A3069"),
			},
			GenericDeleted: ansi.StylePrimitive{
				Color:           stringPtr("#82071E"),
				BackgroundColor: stringPtr("#FFEBE9"),
			},
			GenericEmph: ansi.StylePrimitive{
				Italic: boolPtr(true),
			},
			GenericInserted: ansi.StylePrimitive{
				Color:           stringPtr("#116329"),
				BackgroundColor: stringPtr("#DAFBE1"),
			},
			GenericStrong: ansi.StylePrimitive{
				Bold: boolPtr(true),
			},
			GenericSubheading: ansi.StylePrimitive{
				Color: stringPtr("#6E7781"),
			},
			Background: ansi.StylePrimitive{
				BackgroundColor: stringPtr("#EAEEF2"),
			},
		},
	},
	Table: ansi.StyleTable{
		StyleBlock: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Format:  " ",
				Conceal: boolPtr(true),
			},
		},
		CenterSeparator: stringPtr(""),
		ColumnSeparator: stringPtr(""),
		RowSeparator:    stringPtr(""),
	},
	DefinitionDescription: ansi.StylePrimitive{
		BlockPrefix: " ",
	},
}

func boolPtr(b bool) *bool { return &b }

func stringPtr(s string) *string { return &s }

func uintPtr(u uint) *uint { return &u }

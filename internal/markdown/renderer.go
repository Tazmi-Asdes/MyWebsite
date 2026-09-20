// Package markdown turns the Markdown stored with an article into the
// deterministic, safe representations used by the public site.
package markdown

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/microcosm-cc/bluemonday"
	"github.com/oklog/ulid/v2"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const (
	// RendererVersion is persisted alongside the derived fields.  Change it
	// whenever the derived representation is intentionally changed.
	RendererVersion = "v2"
	// MaxMarkdownBytes is the maximum size of the source Markdown.
	MaxMarkdownBytes = 2 << 20
)

var (
	// ErrInvalidMediaReference is returned when an image destination is not an
	// exact reference to one of this site's media assets.
	ErrInvalidMediaReference = errors.New("invalid_media_reference")
	// ErrImagesNotSupported is kept as a compatibility alias for Stage 1
	// callers.  Images are now supported when they pass the media validation
	// above.
	ErrImagesNotSupported = ErrInvalidMediaReference
	ErrImageAltRequired   = errors.New("image_alt_required")
	ErrImageAltInvalid    = errors.New("image_alt_invalid")
	ErrMarkdownTooLarge   = errors.New("markdown body exceeds 2 MiB")
)

// Renderer is the small contract consumed by the article service.
type Renderer interface {
	Render(markdown string) (Derived, error)
}

// TOCItem is one h2/h3 entry in a derived table of contents.
type TOCItem struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Level int    `json:"level"`
}

// TOC is the stable JSON shape saved with an article.
type TOC struct {
	Items []TOCItem `json:"items"`
}

// MediaReference is one validated image reference in an article.  The order
// is the first-appearance order in the source Markdown.
type MediaReference struct {
	AssetID string `json:"asset_id"`
	AltText string `json:"alt_text"`
}

// MediaRef is a short compatibility alias for MediaReference.
type MediaRef = MediaReference

// Derived contains the values generated from one Markdown source.
type Derived struct {
	HTML            string
	TOCJSON         json.RawMessage
	Preview         string
	RendererVersion string
	MediaReferences []MediaReference
}

// DefaultRenderer is the Markdown implementation of Renderer.
type DefaultRenderer struct {
	markdown goldmark.Markdown
	policy   *bluemonday.Policy
}

// NewRenderer creates the Markdown renderer.
func NewRenderer() Renderer {
	return NewDefaultRenderer()
}

// NewDefaultRenderer creates a concrete renderer.  Returning the concrete
// type is useful to callers that want to keep it in a long-lived service,
// while NewRenderer exposes the smaller domain contract.
func NewDefaultRenderer() *DefaultRenderer {
	return &DefaultRenderer{
		markdown: goldmark.New(
			goldmark.WithExtensions(extension.GFM),
			goldmark.WithRendererOptions(
				renderer.WithNodeRenderers(util.Prioritized(newStage1HTMLRenderer(), 999)),
			),
		),
		policy: newHTMLPolicy(),
	}
}

// Render parses, derives, highlights and sanitizes one Markdown document.
func (r *DefaultRenderer) Render(source string) (Derived, error) {
	if len([]byte(source)) > MaxMarkdownBytes {
		return Derived{}, ErrMarkdownTooLarge
	}

	// Parse once so image validation, heading metadata and preview extraction all
	// use exactly the same AST as the HTML renderer.
	sourceBytes := []byte(source)
	document := r.markdown.Parser().Parse(text.NewReader(sourceBytes))

	mediaReferences := make([]MediaReference, 0)
	mediaByAsset := make(map[string]int)
	if err := ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || node.Kind() != ast.KindImage {
			return ast.WalkContinue, nil
		}
		image := node.(*ast.Image)
		assetID, err := validateMediaDestination(image.Destination)
		if err != nil {
			return ast.WalkStop, err
		}
		altText, err := validateImageAlt(image, sourceBytes)
		if err != nil {
			return ast.WalkStop, err
		}
		if index, ok := mediaByAsset[assetID]; ok {
			if mediaReferences[index].AltText != altText {
				return ast.WalkStop, ErrImageAltInvalid
			}
			return ast.WalkSkipChildren, nil
		}
		mediaByAsset[assetID] = len(mediaReferences)
		mediaReferences = append(mediaReferences, MediaReference{AssetID: assetID, AltText: altText})
		return ast.WalkSkipChildren, nil
	}); err != nil {
		return Derived{}, err
	}

	items := make([]TOCItem, 0)
	usedIDs := make(map[string]int)
	if err := ast.Walk(document, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		heading, ok := node.(*ast.Heading)
		if !ok || (heading.Level != 2 && heading.Level != 3) {
			return ast.WalkContinue, nil
		}
		rawText := visibleText(heading, sourceBytes)
		textValue := normalizeVisibleText(rawText)
		id := uniqueHeadingID(rawText, usedIDs)
		heading.SetAttributeString("id", id)
		items = append(items, TOCItem{ID: id, Text: textValue, Level: heading.Level})
		return ast.WalkSkipChildren, nil
	}); err != nil {
		return Derived{}, err
	}

	tocJSON, err := json.Marshal(TOC{Items: items})
	if err != nil {
		return Derived{}, err
	}

	var rendered bytes.Buffer
	if err := r.markdown.Renderer().Render(&rendered, sourceBytes, document); err != nil {
		return Derived{}, err
	}
	cleanHTML := r.policy.Sanitize(rendered.String())

	preview := previewText(document, sourceBytes)
	return Derived{
		HTML:            cleanHTML,
		TOCJSON:         json.RawMessage(tocJSON),
		Preview:         preview,
		RendererVersion: RendererVersion,
		MediaReferences: mediaReferences,
	}, nil
}

var mediaDestinationPattern = regexp.MustCompile(`^/media/([0-9A-HJKMNP-TV-Z]{26})$`)

func validateMediaDestination(destination []byte) (string, error) {
	matches := mediaDestinationPattern.FindSubmatch(destination)
	if len(matches) != 2 {
		return "", ErrInvalidMediaReference
	}
	assetID := string(matches[1])
	if _, err := ulid.ParseStrict(assetID); err != nil {
		return "", ErrInvalidMediaReference
	}
	return assetID, nil
}

func validateImageAlt(image *ast.Image, source []byte) (string, error) {
	if image.FirstChild() == nil {
		return "", ErrImageAltRequired
	}
	var builder strings.Builder
	for child := image.FirstChild(); child != nil; child = child.NextSibling() {
		if err := appendPlainImageAlt(&builder, child, source); err != nil {
			return "", err
		}
	}
	value := builder.String()
	if value == "" {
		return "", ErrImageAltRequired
	}
	if !utf8.ValidString(value) || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\r\n") {
		return "", ErrImageAltInvalid
	}
	if utf8.RuneCountInString(value) > 300 {
		return "", ErrImageAltInvalid
	}
	return value, nil
}

func appendPlainImageAlt(builder *strings.Builder, node ast.Node, source []byte) error {
	switch typed := node.(type) {
	case *ast.Text:
		if typed.SoftLineBreak() || typed.HardLineBreak() {
			return ErrImageAltInvalid
		}
		builder.Write(typed.Value(source))
		return nil
	case *ast.String:
		builder.Write(typed.Value)
		return nil
	case *ast.AutoLink:
		builder.Write(typed.Text(source))
		return nil
	case *ast.Emphasis, *ast.CodeSpan:
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			if err := appendPlainImageAlt(builder, child, source); err != nil {
				return err
			}
		}
		return nil
	default:
		// Links, raw HTML and nested images are not plain image alt text.
		return ErrImageAltInvalid
	}
}

func uniqueHeadingID(textValue string, used map[string]int) string {
	base := slugify(textValue)
	if base == "" {
		digest := sha256.Sum256([]byte(textValue))
		base = "section-" + hex.EncodeToString(digest[:])[:8]
	}

	candidate := base
	count := 1
	for used[candidate] != 0 {
		count++
		candidate = base + "-" + itoa(count)
	}
	used[candidate] = 1
	return candidate
}

func slugify(value string) string {
	var builder strings.Builder
	pendingHyphen := false
	for _, r := range strings.ToLower(value) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if pendingHyphen && builder.Len() > 0 {
				builder.WriteByte('-')
			}
			builder.WriteRune(r)
			pendingHyphen = false
		case r == '-' || unicode.IsSpace(r):
			if builder.Len() > 0 {
				pendingHyphen = true
			}
		}
	}
	return strings.Trim(builder.String(), "-")
}

// itoa is intentionally tiny: heading suffixes are small and this avoids
// pulling formatting concerns into the slug loop.
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var reversed [20]byte
	i := len(reversed)
	for value > 0 {
		i--
		reversed[i] = byte('0' + value%10)
		value /= 10
	}
	return string(reversed[i:])
}

func normalizeVisibleText(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func visibleText(node ast.Node, source []byte) string {
	var builder strings.Builder
	var visit func(ast.Node)
	visit = func(current ast.Node) {
		switch n := current.(type) {
		case *ast.Text:
			builder.Write(n.Value(source))
			if n.SoftLineBreak() || n.HardLineBreak() {
				builder.WriteByte(' ')
			}
			return
		case *ast.String:
			builder.Write(n.Value)
			return
		case *ast.AutoLink:
			builder.Write(n.Text(source))
			return
		case *ast.RawHTML, *ast.Image:
			return
		}
		for child := current.FirstChild(); child != nil; child = child.NextSibling() {
			visit(child)
		}
	}
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		visit(child)
	}
	return builder.String()
}

func previewText(document ast.Node, source []byte) string {
	var builder strings.Builder
	var visit func(ast.Node)
	visit = func(node ast.Node) {
		switch node.(type) {
		case *ast.Heading, *ast.Image, *ast.FencedCodeBlock, *ast.CodeBlock, *ast.HTMLBlock, *ast.RawHTML:
			return
		}
		switch n := node.(type) {
		case *ast.Text:
			builder.Write(n.Value(source))
			if n.SoftLineBreak() || n.HardLineBreak() {
				builder.WriteByte(' ')
			}
		case *ast.String:
			builder.Write(n.Value)
		case *ast.AutoLink:
			builder.Write(n.Text(source))
		default:
			for child := node.FirstChild(); child != nil; child = child.NextSibling() {
				visit(child)
			}
			if node.Type() == ast.TypeBlock {
				builder.WriteByte('\n')
			}
		}
	}
	visit(document)
	return truncateRunes(normalizeVisibleText(builder.String()), 160)
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

type stage1HTMLRenderer struct {
	formatter *chromahtml.Formatter
}

func newStage1HTMLRenderer() *stage1HTMLRenderer {
	return &stage1HTMLRenderer{
		formatter: chromahtml.New(
			chromahtml.WithClasses(true),
			chromahtml.WithCSSComments(false),
		),
	}
}

func (r *stage1HTMLRenderer) RegisterFuncs(register renderer.NodeRendererFuncRegisterer) {
	register.Register(ast.KindFencedCodeBlock, r.renderFencedCode)
	register.Register(ast.KindHTMLBlock, r.renderHTMLBlock)
	register.Register(ast.KindRawHTML, r.renderRawHTML)
	register.Register(ast.KindImage, r.renderImage)
}

func (r *stage1HTMLRenderer) renderImage(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	image := node.(*ast.Image)
	altText, err := validateImageAlt(image, source)
	if err != nil {
		return ast.WalkSkipChildren, err
	}
	_, _ = writer.WriteString(`<img src="`)
	_, _ = writer.WriteString(html.EscapeString(string(image.Destination)))
	_, _ = writer.WriteString(`" alt="`)
	_, _ = writer.WriteString(html.EscapeString(altText))
	_, _ = writer.WriteString(`">`)
	return ast.WalkSkipChildren, nil
}

func (r *stage1HTMLRenderer) renderFencedCode(writer util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	fenced := node.(*ast.FencedCodeBlock)
	code := string(fenced.Text(source))
	language := strings.TrimSpace(string(fenced.Language(source)))
	if language == "" {
		_, _ = writer.WriteString("<pre><code>")
		_, _ = writer.WriteString(html.EscapeString(code))
		_, _ = writer.WriteString("</code></pre>\n")
		return ast.WalkSkipChildren, nil
	}

	lexer := lexers.Get(language)
	if lexer == nil {
		_, _ = writer.WriteString("<pre><code>")
		_, _ = writer.WriteString(html.EscapeString(code))
		_, _ = writer.WriteString("</code></pre>\n")
		return ast.WalkSkipChildren, nil
	}
	lexer = chroma.Coalesce(lexer)
	tokens, err := lexer.Tokenise(nil, code)
	if err != nil {
		_, _ = writer.WriteString("<pre><code>")
		_, _ = writer.WriteString(html.EscapeString(code))
		_, _ = writer.WriteString("</code></pre>\n")
		return ast.WalkSkipChildren, nil
	}

	var highlighted bytes.Buffer
	if err := r.formatter.Format(&highlighted, styles.Fallback, tokens); err != nil {
		return ast.WalkSkipChildren, err
	}
	output := highlighted.String()
	// The formatter deliberately owns the token classes.  Add a language class
	// only after it has rendered so the formatter remains safe for concurrent
	// requests and never needs mutable per-block state.
	if validLanguageClass(language) {
		output = strings.Replace(output, `<code>`, `<code class="language-`+html.EscapeString(language)+`">`, 1)
	}
	_, _ = writer.WriteString(output)
	return ast.WalkSkipChildren, nil
}

func validLanguageClass(language string) bool {
	for _, r := range language {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_') {
			return false
		}
	}
	return language != ""
}

func (r *stage1HTMLRenderer) renderHTMLBlock(writer util.BufWriter, _ []byte, _ ast.Node, entering bool) (ast.WalkStatus, error) {
	// Raw HTML is not part of the Stage 1 document language.  Returning no
	// output also avoids leaving goldmark's explanatory comments in the result.
	return ast.WalkSkipChildren, nil
}

func (r *stage1HTMLRenderer) renderRawHTML(writer util.BufWriter, _ []byte, _ ast.Node, entering bool) (ast.WalkStatus, error) {
	return ast.WalkSkipChildren, nil
}

var (
	allowedHrefPattern     = regexp.MustCompile(`(?i)^(?:https?://|mailto:)`)
	allowedMediaSrcPattern = regexp.MustCompile(`^/media/[0-9A-HJKMNP-TV-Z]{26}$`)
	allowedIDPattern       = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}_-]*$`)
	allowedClassPattern    = regexp.MustCompile(`^[A-Za-z0-9_-]+(?:\s+[A-Za-z0-9_-]+)*$`)
	checkboxTypePattern    = regexp.MustCompile(`^checkbox$`)
	emptyAttributePattern  = regexp.MustCompile(`^$`)
)

func newHTMLPolicy() *bluemonday.Policy {
	policy := bluemonday.NewPolicy()
	policy.AllowElements(
		"h1", "h2", "h3", "h4", "h5", "h6", "p", "ul", "ol", "li", "a",
		"blockquote", "table", "thead", "tbody", "tfoot", "tr", "th", "td",
		"del", "pre", "code", "span", "input", "img",
	)
	policy.AllowAttrs("id").Matching(allowedIDPattern).OnElements("h2", "h3")
	policy.AllowAttrs("class").Matching(allowedClassPattern).OnElements("pre", "code", "span")
	policy.AllowAttrs("href").Matching(allowedHrefPattern).OnElements("a")
	policy.AllowAttrs("title").OnElements("a")
	policy.AllowAttrs("cite").OnElements("blockquote")
	policy.AllowAttrs("checked", "disabled").Matching(emptyAttributePattern).OnElements("input")
	policy.AllowAttrs("type").Matching(checkboxTypePattern).OnElements("input")
	policy.AllowAttrs("src").Matching(allowedMediaSrcPattern).OnElements("img")
	policy.AllowAttrs("alt").OnElements("img")
	return policy
}

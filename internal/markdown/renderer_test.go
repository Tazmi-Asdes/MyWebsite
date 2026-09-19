package markdown

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestRendererStage1Contract(t *testing.T) {
	renderer := NewDefaultRenderer()
	got, err := renderer.Render("# ignored\n\n## Hello, 世界--Go\n\n## Hello, 世界--Go\n\n### !!!\n\nA **paragraph** with [safe](https://example.com) and [bad](javascript:alert(1)).\n\n- [x] task\n\n~~gone~~\n\n```go\npackage main\n```\n\n![not supported](https://example.com/a.png)")
	if !errors.Is(err, ErrImagesNotSupported) {
		t.Fatalf("Render(image) error = %v, want ErrImagesNotSupported", err)
	}

	got, err = renderer.Render("# ignored\n\n## Hello, 世界--Go\n\n## Hello, 世界--Go\n\n### !!!\n\nParagraph text.\n\n```go\npackage main\n```")
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if !strings.Contains(got.HTML, `<h2 id="hello-世界-go">Hello, 世界--Go</h2>`) {
		t.Fatalf("HTML does not contain stable first heading: %s", got.HTML)
	}
	if !strings.Contains(got.HTML, `id="hello-世界-go-2"`) {
		t.Fatalf("HTML does not contain duplicate heading suffix: %s", got.HTML)
	}
	if !strings.Contains(got.HTML, `id="section-`) {
		t.Fatalf("HTML does not contain hash fallback heading: %s", got.HTML)
	}
	if strings.Contains(got.HTML, `style=`) || strings.Contains(got.HTML, `<script`) {
		t.Fatalf("unsafe output survived: %s", got.HTML)
	}
	if !strings.Contains(got.HTML, `class="language-go"`) || !strings.Contains(got.HTML, `class="kn"`) {
		t.Fatalf("Chroma class output missing: %s", got.HTML)
	}

	var toc TOC
	if err := json.Unmarshal(got.TOCJSON, &toc); err != nil {
		t.Fatalf("TOC is not JSON: %v", err)
	}
	if len(toc.Items) != 3 || toc.Items[0].Level != 2 || toc.Items[2].Level != 3 {
		t.Fatalf("unexpected TOC: %+v", toc)
	}
}

func TestRendererPreviewSkipsHeadingsAndFencedCode(t *testing.T) {
	renderer := NewDefaultRenderer()
	got, err := renderer.Render("## heading\n\nvisible   text\n\n```text\nhidden code\n```\n\nmore text")
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if got.Preview != "visible text more text" {
		t.Fatalf("Preview = %q, want %q", got.Preview, "visible text more text")
	}

	empty, err := renderer.Render("")
	if err != nil {
		t.Fatalf("empty Render() error = %v", err)
	}
	if empty.HTML != "" || empty.Preview != "" || string(empty.TOCJSON) != `{"items":[]}` {
		t.Fatalf("empty derived = %+v", empty)
	}
}

func TestRendererRejectsOversizeAndRawHTML(t *testing.T) {
	renderer := NewDefaultRenderer()
	if _, err := renderer.Render(strings.Repeat("x", MaxMarkdownBytes+1)); !errors.Is(err, ErrMarkdownTooLarge) {
		t.Fatalf("oversize error = %v", err)
	}
	got, err := renderer.Render(`<script>alert(1)</script>\n\n[bad](javascript:alert(1))`)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if strings.Contains(got.HTML, "script") || strings.Contains(got.HTML, "javascript:") {
		t.Fatalf("unsafe Markdown survived: %s", got.HTML)
	}
}

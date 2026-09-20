package markdown

import (
	"errors"
	"strings"
	"testing"
)

const testMediaID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

func TestRendererAcceptsAndDeduplicatesMediaReferences(t *testing.T) {
	renderer := NewDefaultRenderer()
	source := "![A & diagram](/media/" + testMediaID + ")\n\n![A & diagram](/media/" + testMediaID + ")"
	got, err := renderer.Render(source)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if len(got.MediaReferences) != 1 || got.MediaReferences[0].AssetID != testMediaID || got.MediaReferences[0].AltText != "A & diagram" {
		t.Fatalf("media references = %+v", got.MediaReferences)
	}
	if !strings.Contains(got.HTML, `<img src="/media/`+testMediaID+`" alt="A &amp; diagram">`) {
		t.Fatalf("safe image output missing: %s", got.HTML)
	}
	if got.RendererVersion != "v2" {
		t.Fatalf("RendererVersion = %q, want v2", got.RendererVersion)
	}
}

func TestRendererRejectsInvalidMediaAndAlt(t *testing.T) {
	renderer := NewDefaultRenderer()
	tests := []struct {
		name string
		body string
		want error
	}{
		{name: "external", body: "![alt](https://example.com/image.png)", want: ErrInvalidMediaReference},
		{name: "query", body: "![alt](/media/" + testMediaID + "?v=1)", want: ErrInvalidMediaReference},
		{name: "lowercase id", body: "![alt](/media/01arz3ndektsv4rrffq69g5fav)", want: ErrInvalidMediaReference},
		{name: "empty alt", body: "![](/media/" + testMediaID + ")", want: ErrImageAltRequired},
		{name: "whitespace alt", body: "![ ](/media/" + testMediaID + ")", want: ErrImageAltInvalid},
		{name: "leading whitespace", body: "![ alt](/media/" + testMediaID + ")", want: ErrImageAltInvalid},
		{name: "line break", body: "![line\nbreak](/media/" + testMediaID + ")", want: ErrImageAltInvalid},
		{name: "too long", body: "![" + strings.Repeat("x", 301) + "](/media/" + testMediaID + ")", want: ErrImageAltInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := renderer.Render(test.body)
			if !errors.Is(err, test.want) {
				t.Fatalf("Render() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestRendererRejectsConflictingMediaAltAndRawHTML(t *testing.T) {
	renderer := NewDefaultRenderer()
	conflict := "![first](/media/" + testMediaID + ")\n\n![second](/media/" + testMediaID + ")"
	if _, err := renderer.Render(conflict); !errors.Is(err, ErrImageAltInvalid) {
		t.Fatalf("conflicting alt error = %v, want ErrImageAltInvalid", err)
	}
	got, err := renderer.Render(`<img src="https://evil.example/" onerror="alert(1)">

![safe](/media/` + testMediaID + ` "ignored title")`)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if strings.Contains(got.HTML, "evil.example") || strings.Contains(got.HTML, "onerror") || strings.Contains(got.HTML, "title=") {
		t.Fatalf("unsafe image HTML survived: %s", got.HTML)
	}
	if !strings.Contains(got.HTML, `<img src="/media/`+testMediaID+`" alt="safe">`) {
		t.Fatalf("validated image missing: %s", got.HTML)
	}
}

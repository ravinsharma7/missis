package plugin

import (
	"errors"
	"strings"
	"testing"

	"github.com/ravinsharma7/missis/internal/model"
)

func TestRegistrySelectsByRequestMetadataAndCapability(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(RendererRegistration{
		Manifest: Manifest{ID: "plugin.generic", Version: "1"},
		ID:       "generic",
		Selector: Selector{ValueKind: model.ValueKind("example/card")},
		Render: func(Request) (RenderResult, error) {
			return RenderResult{Lines: []string{"generic"}}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(RendererRegistration{
		Manifest:             Manifest{ID: "plugin.project", Version: "1"},
		ID:                   "project-card",
		Selector:             Selector{ValueKind: model.ValueKind("example/card"), DeclaredSchema: "project/card"},
		RequiredCapabilities: []string{"terminal.card"},
		Render: func(Request) (RenderResult, error) {
			return RenderResult{Lines: []string{"project"}}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}

	withoutCapability, err := registry.Render(Request{
		ValueKind:      model.ValueKind("example/card"),
		DeclaredSchema: "project/card",
		Value:          model.Value{Kind: model.ValueKind("example/card"), Text: "raw"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if withoutCapability.Renderer != "plugin.generic/generic" || withoutCapability.Fallback {
		t.Fatalf("without capability = %+v", withoutCapability)
	}

	withCapability, err := registry.Render(Request{
		ValueKind:      model.ValueKind("example/card"),
		DeclaredSchema: "project/card",
		Capabilities:   []string{"terminal.card"},
		Value:          model.Value{Kind: model.ValueKind("example/card"), Text: "raw"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if withCapability.Renderer != "plugin.project/project-card" || withCapability.Fallback {
		t.Fatalf("with capability = %+v", withCapability)
	}
}

func TestRegistryRejectsAmbiguousMatches(t *testing.T) {
	registry := NewRegistry()
	for _, id := range []string{"a", "b"} {
		if err := registry.Register(RendererRegistration{
			Manifest: Manifest{ID: "plugin." + id, Version: "1"},
			ID:       "renderer",
			Selector: Selector{ValueKind: model.ValueKind("example/value")},
			Render: func(Request) (RenderResult, error) {
				return RenderResult{Lines: []string{"unexpected"}}, nil
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := registry.Render(Request{
		ValueKind: model.ValueKind("example/value"),
		Value:     model.Value{Kind: model.ValueKind("example/value"), Text: "raw"},
	})
	if err == nil || !strings.Contains(err.Error(), "ambiguous renderer selection") {
		t.Fatalf("error = %v", err)
	}
}

func TestFallbackReturnsDataWithoutSemanticDecoration(t *testing.T) {
	registry := NewRegistry()
	value := model.Value{
		Kind: model.ValueKindJSON,
		Data: map[string]any{"uri": "artifact:sha256:abc", "kind": "image"},
	}
	result, err := registry.Render(Request{ValueKind: value.Kind, Value: value})
	if !errors.Is(err, ErrKnownFallback) {
		t.Fatalf("error = %v, want known fallback", err)
	}
	if result.State != RenderStateKnownFallback || !result.Fallback || len(result.Lines) != 1 {
		t.Fatalf("result = %+v", result)
	}
	if result.Lines[0] != `{"kind":"image","uri":"artifact:sha256:abc"}` {
		t.Fatalf("fallback data = %q", result.Lines[0])
	}
	if result.Diagnostic == "" {
		t.Fatal("known fallback must carry an out-of-band diagnostic")
	}
}

func TestMarkdownFallbackIsTheOriginalMarkdown(t *testing.T) {
	content := "# Heading\n\nraw *markdown*"
	result, err := NewRegistry().Render(Request{
		ValueKind: model.ValueKindMarkdown,
		Value:     model.Value{Kind: model.ValueKindMarkdown, Text: content},
	})
	if !errors.Is(err, ErrKnownFallback) {
		t.Fatalf("error = %v, want known fallback", err)
	}
	if strings.Join(result.Lines, "\n") != content {
		t.Fatalf("fallback content = %q, want original Markdown", strings.Join(result.Lines, "\n"))
	}
}

func TestUnknownKindIsAnErrorInsteadOfAHeuristicFallback(t *testing.T) {
	result, err := NewRegistry().Render(Request{
		ValueKind: model.ValueKind("plugin/unknown"),
		Value:     model.Value{Kind: model.ValueKind("plugin/unknown"), Data: map[string]any{"value": "raw"}},
	})
	if !errors.Is(err, ErrUnsupportedValueKind) {
		t.Fatalf("error = %v, want unsupported value kind", err)
	}
	if result.State != RenderStateUnsupported || result.Fallback || len(result.Lines) != 0 {
		t.Fatalf("unsupported result = %+v", result)
	}
}

func TestRendererFailureIsNotConvertedToFallback(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(RendererRegistration{
		Manifest: Manifest{ID: "plugin.failing", Version: "1"},
		ID:       "renderer",
		Selector: Selector{ValueKind: model.ValueKindText},
		Render: func(Request) (RenderResult, error) {
			return RenderResult{}, errors.New("renderer broke")
		},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := registry.Render(Request{
		ValueKind: model.ValueKindText,
		Value:     model.Value{Kind: model.ValueKindText, Text: "original"},
	})
	if err == nil || errors.Is(err, ErrKnownFallback) || !strings.Contains(err.Error(), "renderer broke") {
		t.Fatalf("error = %v, want renderer failure", err)
	}
	if result.Fallback || len(result.Lines) != 0 {
		t.Fatalf("renderer failure was converted to fallback: %+v", result)
	}
}

func TestRendererOutputSanitizesTerminalControls(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(RendererRegistration{
		Manifest: Manifest{ID: "plugin.test", Version: "1"},
		ID:       "renderer",
		Render: func(Request) (RenderResult, error) {
			return RenderResult{Lines: []string{"safe\x1b[31m"}}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := registry.Render(Request{Value: model.Value{Text: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Lines[0] != "safe [31m" {
		t.Fatalf("sanitized lines = %q", result.Lines)
	}
}

func TestRunProcessorAttachesInvocation(t *testing.T) {
	input := ProcessorInput{Invocation: model.InvocationRef{ID: "run:processor/1"}}
	output, err := RunProcessor(func(ProcessorInput) (ProcessorOutput, error) {
		return ProcessorOutput{Events: []model.Event{{ID: "event:derived"}}}, nil
	}, input)
	if err != nil {
		t.Fatal(err)
	}
	if output.Events[0].Invocation == nil || output.Events[0].Invocation.ID != input.Invocation.ID {
		t.Fatalf("event provenance = %+v", output.Events[0].Invocation)
	}
}

func TestRunProcessorRejectsMismatchedInvocation(t *testing.T) {
	_, err := RunProcessor(func(ProcessorInput) (ProcessorOutput, error) {
		return ProcessorOutput{Events: []model.Event{{Invocation: &model.InvocationRef{ID: "run:other/1"}}}}, nil
	}, ProcessorInput{Invocation: model.InvocationRef{ID: "run:processor/1"}})
	if err == nil || !strings.Contains(err.Error(), "has invocation") {
		t.Fatalf("error = %v", err)
	}
}

func TestProcessorRegistrySelectsByMetadata(t *testing.T) {
	registry := NewProcessorRegistry()
	if err := registry.Register(ProcessorRegistration{
		Manifest: Manifest{ID: "plugin.processor", Version: "1"},
		ID:       "derive-card",
		Selector: Selector{ValueKind: model.ValueKind("example/card"), DeclaredSchema: "project/card"},
		Process: func(input ProcessorInput) (ProcessorOutput, error) {
			return ProcessorOutput{Events: []model.Event{{ID: "event:derived"}}}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	output, selected, err := registry.Run(ProcessorInput{
		Invocation:     model.InvocationRef{ID: "run:processor/2"},
		PartPath:       "evidence/card",
		DeclaredSchema: "project/card",
		Value:          model.Value{Kind: model.ValueKind("example/card")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if selected != "plugin.processor/derive-card" || len(output.Events) != 1 {
		t.Fatalf("selected=%q output=%+v", selected, output)
	}
}

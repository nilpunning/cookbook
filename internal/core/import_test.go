package core

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/fake"
)

func TestTrimCompletion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty",
			input:    "",
			expected: "",
		},
		{
			name:     "null json with newlines",
			input:    "```json\nnull\n```",
			expected: "null",
		},
		{
			name:     "null json without newlines",
			input:    "```jsonnull```",
			expected: "null",
		},
		{
			name:     "null no quote",
			input:    "null",
			expected: "null",
		},

		{
			name:     "json with newlines",
			input:    "```json\n{\"hi\":7}\n```",
			expected: "{\"hi\":7}",
		},
		{
			name:     "json without newlines",
			input:    "```json{\"hi\":7}```",
			expected: "{\"hi\":7}",
		},
		{
			name:     "no quote",
			input:    "{\"hi\":7}",
			expected: "{\"hi\":7}",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := TrimCompletion(test.input)
			if result != test.expected {
				t.Errorf("expected %q, got %q", test.expected, result)
			}
		})
	}
}

// TODO: take downloaded html for this test and anonymize it
// func TestStripExtraneousHTML(t *testing.T) {
// 	downloaded_reader, err := os.Open("testdata/downloaded.html")
// 	if err != nil {
// 		t.Fatalf("failed to open file: %v", err)
// 	}
// 	defer downloaded_reader.Close()
// 	downloaded, err := StripExtraneousHTML(downloaded_reader)
// 	if err != nil {
// 		t.Fatalf("failed to strip html: %v", err)
// 	}

// 	stripped_bytes, err := os.ReadFile("testdata/stripped.html")
// 	if err != nil {
// 		t.Fatalf("failed to open file: %v", err)
// 	}

// 	if downloaded != string(stripped_bytes) {
// 		t.Fatalf("expected stripped html to be equal to original html")
// 	}
// }

func fakeRequest(_ context.Context, _ string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func TestImport(t *testing.T) {
	// t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	name := "Fake Recipe"
	body := "Fake recipe body"
	success := fmt.Sprintf("```json\n"+`{"name": "%s", "body": "%s"}`+"```", name, body)

	successTest := func(llm llms.Model) {
		recipe, err := Import(ctx, llm, fakeRequest, "https://example.com/recipe")
		if err != nil {
			t.Errorf("import failed: %v", err)
			return
		}
		if recipe == nil {
			t.Error("expected recipe, got nil")
			return
		}
		if recipe.Name != name {
			t.Errorf("expected name 'Fake Recipe', got '%s'", recipe.Name)
		}
		if recipe.Body != body {
			t.Errorf("expected body 'Fake recipe body', got '%s'", recipe.Body)
		}
	}

	t.Run("crawl success", func(t *testing.T) {
		successTest(fake.NewFakeLLM([]string{success}))
	})

	t.Run("crawl failure, request success", func(t *testing.T) {
		successTest(fake.NewFakeLLM([]string{"null", success}))
	})

	t.Run("fail", func(t *testing.T) {
		fakeLLM := fake.NewFakeLLM([]string{"null", "null"})
		recipe, err := Import(ctx, fakeLLM, fakeRequest, "https://example.com/recipe")
		if err != nil {
			return
		}
		if recipe != nil {
			t.Error("got unexpected recipe")
			return
		}
	})
}

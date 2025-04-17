package core

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"encoding/json"

	"github.com/PuerkitoBio/goquery"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/googleai"

	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"

	"golang.org/x/exp/slog"
	"golang.org/x/net/context"
	"golang.org/x/net/html"
)

type Recipe struct {
	Name string `json:"name"`
	Body string `json:"body"`
}

const CRAWLED_PARSE_PROMPT = `
  Consider this recipe URL: %s.
  The output should be JSON formatted with the following schema:
  {
    "type": "object",
    "nullable": true,
    "description": "A recipe.  The object should be null if you have never crawled the provided URL.",
    "properties": {
    	"name": {
    		"type": "string",
    		"description": "The name of the recipe"
    	},
    	"body": {
    		"type": "string",
    		"description": "The recipe ingredients and directions in markdown format.  Do not include the recipe name in the body."
    	}
    },
    "required": ["name", "body"]
  }
`

const FETCHED_PARSE_PROMPT = `
  Consider this recipe:
  <html>
  	%s
  </html>
  The output should be JSON formatted with the following schema:
  {
    "type": "object",
    "nullable": true,
    "description": "A recipe.  The object should be null if you have not found a recipe.",
    "properties": {
    	"name": {
    		"type": "string",
    		"description": "The name of the recipe"
    	},
    	"body": {
    		"type": "string",
    		"description": "The recipe ingredients and directions in markdown format.  Do not include the recipe name in the body."
    	}
    },
    "required": ["name", "body"]
  }
`

func TrimCompletion(completion string) string {
	completion = strings.TrimPrefix(completion, "```json")
	completion = strings.TrimSuffix(completion, "```")
	return strings.TrimSpace(completion)
}

func QueryLLM(ctx context.Context, llm llms.Model, prompt string) (*Recipe, error) {
	completion, err := llms.GenerateFromSinglePrompt(ctx, llm, prompt)
	if err != nil {
		return nil, fmt.Errorf("error generating content: %v", err)
	}

	slog.Info("LLM", "request", prompt, "response", completion)

	completion = TrimCompletion(completion)

	if completion == "null" {
		return nil, nil
	}

	var recipe Recipe
	if err := json.Unmarshal([]byte(completion), &recipe); err != nil {
		return nil, fmt.Errorf("error parsing response: %v", err)
	}

	return &recipe, nil
}

func StripExtraneousHTML(reader io.Reader) (string, error) {
	doc, err := goquery.NewDocumentFromReader(reader)
	if err != nil {
		return "", err
	}

	// https://github.com/PuerkitoBio/goquery/issues/139
	doc.Find("noscript").Each(func(i int, selection *goquery.Selection) {
		selection.ReplaceWithHtml(selection.Text())
	}).End()

	doc.Find("head,script,style,link,svg").Remove().End()

	doc.Contents().FilterFunction(func(i int, s *goquery.Selection) bool {
		return s.Get(0).Type == html.CommentNode
	}).Remove().End()

	clean := func(i int, s *goquery.Selection) {
		if n := s.Get(0); n != nil {
			n.Data = strings.TrimSpace(n.Data)
			n.Attr = nil
		}
	}

	doc.Find("*").Each(clean)
	doc.Find("*").Contents().Each(clean)

	return doc.Html()
}

type LLMNotFoundError struct{ LLM string }

func (u *LLMNotFoundError) Error() string {
	return fmt.Sprintf("llm not found: %s", u.LLM)
}

func LLMModel(ctx context.Context, config Config) (llms.Model, error) {
	if config.Server.LLM == nil {
		return nil, &LLMNotFoundError{LLM: "unknown"}
	}

	switch *config.Server.LLM {

	// https://github.com/tmc/langchaingo/blob/main/llms/googleai/option.go
	case "Google":
		options := []googleai.Option{}

		if config.Google.APIKey != nil {
			options = append(options, googleai.WithAPIKey(*config.Google.APIKey))
		}

		if config.Google.Model != nil {
			options = append(options, googleai.WithDefaultModel(*config.Google.Model))
		}

		return googleai.New(ctx, options...)

	// https://github.com/tmc/langchaingo/blob/main/llms/ollama/options.go
	case "Ollama":
		options := []ollama.Option{}

		if config.Ollama.ServerURL != nil {
			options = append(options, ollama.WithServerURL(*config.Ollama.ServerURL))
		}

		if config.Ollama.Model != nil {
			options = append(options, ollama.WithModel(*config.Ollama.Model))
		}

		return ollama.New(options...)

	// https://github.com/tmc/langchaingo/blob/main/llms/openai/openaillm_option.go
	case "OpenAI":
		options := []openai.Option{}

		if config.OpenAI.Token != nil {
			options = append(options, openai.WithToken(*config.OpenAI.Token))
		}

		if config.OpenAI.BaseURL != nil {
			options = append(options, openai.WithBaseURL(*config.OpenAI.BaseURL))
		}

		if config.OpenAI.Model != nil {
			options = append(options, openai.WithModel(*config.OpenAI.Model))
		}

		return openai.New(options...)
	}

	return nil, &LLMNotFoundError{LLM: *config.Server.LLM}
}

type Request func(ctx context.Context, url string) (io.ReadCloser, error)

func HTTPRequest(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	return resp.Body, err
}

func Import(ctx context.Context, llm llms.Model, request Request, url string) (*Recipe, error) {
	crawledChan := make(chan struct {
		*Recipe
		error
	}, 1)

	go func() {
		result, err := QueryLLM(ctx, llm, fmt.Sprintf(CRAWLED_PARSE_PROMPT, url))
		crawledChan <- struct {
			*Recipe
			error
		}{result, err}
	}()

	requestChan := make(chan struct {
		string
		error
	}, 1)

	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel() // cancels GET if LLM result is positive

	go func() {
		str := ""
		readCloser, err := request(requestCtx, url)
		if err == nil {
			defer readCloser.Close()
			str, err = StripExtraneousHTML(readCloser)
		}
		slog.Info("request", "url", url, "str", str, "error", err)
		requestChan <- struct {
			string
			error
		}{str, err}
	}()

	crawlResult := <-crawledChan
	if crawlResult.error != nil {
		return nil, crawlResult.error
	}
	if crawlResult.Recipe != nil {
		return crawlResult.Recipe, nil
	}

	requestResult := <-requestChan
	if requestResult.error != nil {
		return nil, requestResult.error
	}

	return QueryLLM(ctx, llm, fmt.Sprintf(FETCHED_PARSE_PROMPT, requestResult.string))
}

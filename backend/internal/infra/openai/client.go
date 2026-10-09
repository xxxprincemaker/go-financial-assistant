package openai

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/MarcosAAlbanoJunior/go-financial-assistant/internal/domain/ports"
	"github.com/MarcosAAlbanoJunior/go-financial-assistant/internal/infra/gemini"
	openaisdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

const defaultModel = "gpt-6-luna"

// Client implementa as interfaces de análise e Coach usando a Responses API.
type Client struct {
	mu      sync.RWMutex
	client  *openaisdk.Client
	apiKey  string
	model   string
	baseURL string
}

func NewClient(apiKey, model string) *Client {
	return newClient(apiKey, model, "")
}

func newClient(apiKey, model, baseURL string) *Client {
	c := &Client{model: strings.TrimSpace(model), baseURL: baseURL}
	c.SetAPIKey(apiKey)
	return c
}

func (c *Client) sdkClient(apiKey string) *openaisdk.Client {
	opts := []option.RequestOption{option.WithAPIKey(apiKey)}
	if c.baseURL != "" {
		opts = append(opts, option.WithBaseURL(c.baseURL))
	}
	client := openaisdk.NewClient(opts...)
	return &client
}

// SetAPIKey aplica a credencial imediatamente. Uma chave vazia desliga o cliente.
func (c *Client) SetAPIKey(apiKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == c.apiKey {
		return
	}
	c.apiKey = apiKey
	if apiKey == "" {
		c.client = nil
		return
	}
	c.client = c.sdkClient(apiKey)
}

func (c *Client) SetModel(model string) {
	c.mu.Lock()
	c.model = strings.TrimSpace(model)
	c.mu.Unlock()
}

func (c *Client) Ready() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.client != nil
}

func (c *Client) Name() string { return "OpenAI" }

func (c *Client) snapshot() (*openaisdk.Client, string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.client == nil {
		return nil, "", ports.ErrAIUnavailable
	}
	model := c.model
	if model == "" {
		model = defaultModel
	}
	return c.client, model, nil
}

func (c *Client) generate(ctx context.Context, instructions string, input responses.ResponseNewParamsInputUnion, schema map[string]any) (string, error) {
	client, model, err := c.snapshot()
	if err != nil {
		return "", err
	}
	request := responses.ResponseNewParams{
		Model:           shared.ResponsesModel(model),
		Instructions:    param.NewOpt(instructions),
		Input:           input,
		MaxOutputTokens: param.NewOpt[int64](4096),
		Store:           param.NewOpt(false),
	}
	if schema != nil {
		request.Text = responses.ResponseTextConfigParam{
			Format: responses.ResponseFormatTextConfigUnionParam{
				OfJSONSchema: &responses.ResponseFormatTextJSONSchemaConfigParam{
					Name:   "financial_assistant_response",
					Schema: schema,
					Strict: param.NewOpt(true),
				},
			},
		}
	}
	resp, err := client.Responses.New(ctx, request)
	if err != nil {
		return "", fmt.Errorf("erro ao chamar OpenAI: %w", err)
	}
	if resp == nil {
		return "", errors.New("OpenAI retornou resposta vazia")
	}
	text := strings.TrimSpace(resp.OutputText())
	if text == "" {
		return "", errors.New("OpenAI retornou conteúdo vazio")
	}
	return text, nil
}

func textInput(text string) responses.ResponseNewParamsInputUnion {
	return responses.ResponseNewParamsInputUnion{OfString: param.NewOpt(text)}
}

func contentInput(content ...responses.ResponseInputContentUnionParam) responses.ResponseNewParamsInputUnion {
	message := responses.EasyInputMessageParam{
		Content: responses.EasyInputMessageContentUnionParam{
			OfInputItemContentList: responses.ResponseInputMessageContentListParam(content),
		},
		Role: responses.EasyInputMessageRole("user"),
		Type: responses.EasyInputMessageType("message"),
	}
	return responses.ResponseNewParamsInputUnion{
		OfInputItemList: responses.ResponseInputParam{{OfMessage: &message}},
	}
}

func dataURL(data []byte, mimeType string) string {
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func (c *Client) AnalyzeText(ctx context.Context, text string) (*ports.ExpenseAnalysis, error) {
	prompt := "Analise esta entrada financeira e extraia as informações:\n\n" + text
	raw, err := c.generate(ctx, gemini.SystemPrompt, textInput(prompt), expenseSchema())
	if err != nil {
		return nil, err
	}
	return gemini.ParseAnalysisJSON(raw)
}

func (c *Client) AnalyzeImage(ctx context.Context, imageData []byte, mimeType string) (*ports.ExpenseAnalysis, error) {
	input := contentInput(
		responses.ResponseInputContentUnionParam{OfInputImage: &responses.ResponseInputImageParam{ImageURL: param.NewOpt(dataURL(imageData, mimeType)), Detail: "auto"}},
		responses.ResponseInputContentUnionParam{OfInputText: &responses.ResponseInputTextParam{Text: "Analise esta nota fiscal ou recibo e extraia as informações da despesa."}},
	)
	raw, err := c.generate(ctx, gemini.SystemPrompt, input, expenseSchema())
	if err != nil {
		return nil, err
	}
	return gemini.ParseAnalysisJSON(raw)
}

func (c *Client) AnalyzeDocument(ctx context.Context, data []byte, mimeType string) (*ports.StatementAnalysis, error) {
	if mimeType == "" {
		mimeType = "application/pdf"
	}
	filename := "statement"
	if strings.Contains(mimeType, "pdf") {
		filename += ".pdf"
	} else {
		filename += ".txt"
	}
	input := contentInput(
		responses.ResponseInputContentUnionParam{OfInputFile: &responses.ResponseInputFileParam{Filename: param.NewOpt(filename), FileData: param.NewOpt(dataURL(data, mimeType))}},
		responses.ResponseInputContentUnionParam{OfInputText: &responses.ResponseInputTextParam{Text: "Extraia todas as transações deste extrato bancário."}},
	)
	raw, err := c.generate(ctx, gemini.StatementPrompt, input, statementSchema())
	if err != nil {
		return nil, err
	}
	return gemini.ParseStatementJSON(raw)
}

func (c *Client) Advise(ctx context.Context, contextJSON []byte) (ports.CoachAdvice, error) {
	raw, err := c.generate(ctx, gemini.CoachSystemPrompt(), textInput("Dados:\n"+string(contextJSON)), coachSchema())
	if err != nil {
		return ports.CoachAdvice{}, err
	}
	return gemini.ParseCoachJSON(raw)
}

func (c *Client) Categorize(ctx context.Context, contextJSON []byte) ([]ports.CategorySuggestion, error) {
	raw, err := c.generate(ctx, gemini.CategorizeSystemPrompt(), textInput("Dados:\n"+string(contextJSON)), categorySchema())
	if err != nil {
		return nil, err
	}
	return gemini.ParseCategoriesJSON(raw)
}

func Ping(ctx context.Context, apiKey, model string) error {
	c := NewClient(apiKey, model)
	_, err := c.generate(ctx, "Responda apenas: ok", textInput("ok"), nil)
	return err
}

func (c *Client) Close() error { return nil }

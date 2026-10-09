package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const expenseJSON = `{"type":"SINGLE","amount":45,"description":"Almoço","category":"FOOD","payment_method":"PIX","confidence":0.95,"transfer_direction":null,"installments":null,"recurring":null,"cancel_recurring":null,"query":null,"export":null}`

func responseBody(output string) string {
	encoded, _ := json.Marshal(output)
	return `{"id":"resp_test","created_at":1,"model":"gpt-4o-mini","status":"completed","output":[{"id":"msg_test","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":` + string(encoded) + `,"annotations":[]}]}]}`
}

func fakeResponsesServer(t *testing.T, status int, output string) (*httptest.Server, *string, *string) {
	t.Helper()
	var requestBody, requestPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPath = r.URL.Path
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("ler request: %v", err)
		}
		requestBody = string(body)
		if r.Method != http.MethodPost {
			t.Errorf("método = %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer api-key" {
			t.Errorf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status >= 400 {
			_, _ = io.WriteString(w, `{"error":{"message":"rate limit","type":"rate_limit_error","code":null,"param":null}}`)
			return
		}
		_, _ = io.WriteString(w, output)
	}))
	return server, &requestBody, &requestPath
}

func TestAnalyzeTextUsesResponsesAndStrictStructuredOutput(t *testing.T) {
	server, body, path := fakeResponsesServer(t, http.StatusOK, responseBody(expenseJSON))
	defer server.Close()
	client := newClient("api-key", "gpt-test", server.URL)
	got, err := client.AnalyzeText(context.Background(), "gastei 45 no almoço")
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != "SINGLE" || got.Amount == nil || *got.Amount != 45 || got.Description == nil || *got.Description != "Almoço" {
		t.Fatalf("análise = %+v", got)
	}
	if !strings.HasSuffix(*path, "/responses") || !strings.Contains(*body, `"model":"gpt-test"`) || !strings.Contains(*body, `"store":false`) {
		t.Fatalf("request Responses: path=%q body=%s", *path, *body)
	}
	if !strings.Contains(*body, `"type":"json_schema"`) || !strings.Contains(*body, `"strict":true`) || !strings.Contains(*body, `"additionalProperties":false`) {
		t.Fatalf("schema estruturado estrito ausente: %s", *body)
	}
	if !strings.Contains(*body, "gastei 45 no almoço") {
		t.Fatalf("texto não enviado: %s", *body)
	}
}

func TestAnalyzeImageAndPDFUseResponsesFileInputs(t *testing.T) {
	server, body, _ := fakeResponsesServer(t, http.StatusOK, responseBody(expenseJSON))
	defer server.Close()
	client := newClient("api-key", "gpt-test", server.URL)
	if _, err := client.AnalyzeImage(context.Background(), []byte{1, 2, 3}, "image/png"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(*body, `"type":"input_image"`) || !strings.Contains(*body, `data:image/png;base64,AQID`) {
		t.Fatalf("imagem enviada em formato compatível: %s", *body)
	}

	pdfServer, pdfBody, _ := fakeResponsesServer(t, http.StatusOK, responseBody(`{"transactions":[{"date":"2026-01-02","raw_description":"PADARIA","description":"Padaria","amount":12.5,"kind":"EXPENSE","direction":null,"category":"FOOD","payment_method":"PIX"}]}`))
	defer pdfServer.Close()
	client = newClient("api-key", "gpt-test", pdfServer.URL)
	statement, err := client.AnalyzeDocument(context.Background(), []byte{1, 2, 3}, "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	if len(statement.Transactions) != 1 || statement.Transactions[0].Description != "Padaria" || statement.Transactions[0].Amount != 12.5 {
		t.Fatalf("transações extraídas do PDF: %+v", statement.Transactions)
	}
	if !strings.Contains(*pdfBody, `"type":"input_file"`) || !strings.Contains(*pdfBody, `"filename":"statement.pdf"`) || !strings.Contains(*pdfBody, `data:application/pdf;base64,AQID`) {
		t.Fatalf("PDF enviado como arquivo: %s", *pdfBody)
	}
}

func TestCoachAndCategoriesUseStructuredOutputs(t *testing.T) {
	coachServer, coachBody, _ := fakeResponsesServer(t, http.StatusOK, responseBody(`{"resumo":"Revise as contas recorrentes.","acoes":[],"metas":[],"perguntas":[]}`))
	defer coachServer.Close()
	client := newClient("api-key", "gpt-test", coachServer.URL)
	advice, err := client.Advise(context.Background(), []byte(`{"mes":"2026-10"}`))
	if err != nil || advice.Summary != "Revise as contas recorrentes." {
		t.Fatalf("Coach: %+v, %v", advice, err)
	}
	if !strings.Contains(*coachBody, `"type":"json_schema"`) || !strings.Contains(*coachBody, `"strict":true`) {
		t.Fatalf("saída estruturada do Coach não foi solicitada: %s", *coachBody)
	}

	categoriesServer, categoriesBody, _ := fakeResponsesServer(t, http.StatusOK, responseBody(`{"sugestoes":[{"id":"c1","categoria":"FOOD"}]}`))
	defer categoriesServer.Close()
	client = newClient("api-key", "gpt-test", categoriesServer.URL)
	categories, err := client.Categorize(context.Background(), []byte(`{"contas":[]}`))
	if err != nil || len(categories) != 1 || categories[0].ID != "c1" || categories[0].Category != "FOOD" {
		t.Fatalf("categorias: %+v, %v", categories, err)
	}
	if !strings.Contains(*categoriesBody, `"type":"json_schema"`) || !strings.Contains(*categoriesBody, `"strict":true`) {
		t.Fatalf("saída estruturada de categorias não foi solicitada: %s", *categoriesBody)
	}
}

func TestResponsesEmptyOutputAndAPIError(t *testing.T) {
	for name, tc := range map[string]struct {
		status int
		body   string
	}{
		"vazia":       {http.StatusOK, `{"id":"resp_empty","created_at":1,"model":"gpt-4o-mini","status":"completed","output":[]}`},
		"erro da API": {http.StatusTooManyRequests, ""},
	} {
		t.Run(name, func(t *testing.T) {
			server, _, _ := fakeResponsesServer(t, tc.status, tc.body)
			defer server.Close()
			client := newClient("api-key", "gpt-test", server.URL)
			if _, err := client.AnalyzeText(context.Background(), "teste"); err == nil {
				t.Fatal("esperava erro para saída vazia ou erro HTTP")
			}
		})
	}
}

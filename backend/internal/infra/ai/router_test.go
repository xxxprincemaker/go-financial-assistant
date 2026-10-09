package ai

import (
	"context"
	"errors"
	"testing"

	"github.com/MarcosAAlbanoJunior/go-financial-assistant/internal/domain/ports"
)

type fakeProvider struct {
	name        string
	ready       bool
	textCalls   int
	adviseCalls int
	err         error
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Ready() bool  { return f.ready }
func (f *fakeProvider) Close() error { return nil }
func (f *fakeProvider) AnalyzeText(context.Context, string) (*ports.ExpenseAnalysis, error) {
	f.textCalls++
	return &ports.ExpenseAnalysis{Description: stringPtr(f.name)}, f.err
}
func (f *fakeProvider) AnalyzeImage(context.Context, []byte, string) (*ports.ExpenseAnalysis, error) {
	return &ports.ExpenseAnalysis{Description: stringPtr(f.name)}, f.err
}

func stringPtr(s string) *string { return &s }
func (f *fakeProvider) AnalyzeDocument(context.Context, []byte, string) (*ports.StatementAnalysis, error) {
	return &ports.StatementAnalysis{}, f.err
}
func (f *fakeProvider) Advise(context.Context, []byte) (ports.CoachAdvice, error) {
	f.adviseCalls++
	return ports.CoachAdvice{Summary: f.name}, f.err
}
func (f *fakeProvider) Categorize(context.Context, []byte) ([]ports.CategorySuggestion, error) {
	return []ports.CategorySuggestion{{ID: f.name}}, f.err
}

func TestRouterSwitchesProviderImmediatelyAcrossFeatures(t *testing.T) {
	gemini := &fakeProvider{name: "Google Gemini", ready: true}
	openai := &fakeProvider{name: "OpenAI", ready: true}
	router := NewRouter("gemini", gemini, openai)

	if got, err := router.AnalyzeText(context.Background(), "texto"); err != nil || got.Description == nil || *got.Description != "Google Gemini" {
		t.Fatalf("análise com Gemini: %#v %v", got, err)
	}
	if _, err := router.Advise(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	router.SetProvider("openai")
	if router.ProviderName() != "OpenAI" || !router.Ready() {
		t.Fatalf("provedor ativo após troca: %s, ready=%v", router.ProviderName(), router.Ready())
	}
	if got, err := router.AnalyzeText(context.Background(), "texto"); err != nil || got.Description == nil || *got.Description != "OpenAI" {
		t.Fatalf("análise com OpenAI: %#v %v", got, err)
	}
	if got, err := router.Advise(context.Background(), nil); err != nil || got.Summary != "OpenAI" {
		t.Fatalf("Coach com OpenAI: %#v %v", got, err)
	}
	if gemini.textCalls != 1 || gemini.adviseCalls != 1 || openai.textCalls != 1 || openai.adviseCalls != 1 {
		t.Fatalf("chamadas não seguiram a seleção: Gemini=%+v OpenAI=%+v", gemini, openai)
	}
}

func TestRouterDoesNotFallbackWhenSelectedProviderUnavailableOrFails(t *testing.T) {
	gemini := &fakeProvider{name: "Google Gemini", ready: true}
	openai := &fakeProvider{name: "OpenAI", ready: false}
	router := NewRouter("openai", gemini, openai)
	if router.Ready() {
		t.Fatal("provedor selecionado sem chave não está pronto")
	}
	if _, err := router.AnalyzeText(context.Background(), "texto"); !errors.Is(err, ports.ErrAIUnavailable) {
		t.Fatalf("sem chave deve retornar indisponível, sem fallback: %v", err)
	}
	if gemini.textCalls != 0 {
		t.Fatal("não pode chamar Gemini como fallback")
	}

	openai.ready = true
	openai.err = errors.New("falha do provedor")
	if _, err := router.AnalyzeText(context.Background(), "texto"); err == nil || gemini.textCalls != 0 {
		t.Fatalf("erro do provedor ativo não pode acionar fallback: %v", err)
	}
}

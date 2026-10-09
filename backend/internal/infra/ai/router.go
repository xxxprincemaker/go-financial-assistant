package ai

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/MarcosAAlbanoJunior/go-financial-assistant/internal/domain/ports"
)

type Provider interface {
	ports.AIAnalyzer
	ports.Coach
	Ready() bool
	Name() string
	Close() error
}

// Router expõe um provedor ativo para o restante do app e permite trocá-lo em execução.
type Router struct {
	mu        sync.RWMutex
	providers map[string]Provider
	active    string
}

func NewRouter(active string, providers ...Provider) *Router {
	r := &Router{providers: make(map[string]Provider)}
	for _, p := range providers {
		if p == nil {
			continue
		}
		switch strings.ToLower(p.Name()) {
		case "google gemini", "gemini":
			r.providers["gemini"] = p
		case "openai":
			r.providers["openai"] = p
		}
	}
	r.SetProvider(active)
	return r
}

func (r *Router) SetProvider(name string) {
	r.mu.Lock()
	r.active = strings.ToLower(strings.TrimSpace(name))
	r.mu.Unlock()
}

func (r *Router) selected() Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.providers[r.active]
}

func (r *Router) available() Provider {
	p := r.selected()
	if p == nil || !p.Ready() {
		return nil
	}
	return p
}

func (r *Router) ProviderName() string {
	if p := r.selected(); p != nil {
		return p.Name()
	}
	return "Provedor de IA não configurado"
}

func (r *Router) Ready() bool {
	return r.available() != nil
}

func (r *Router) AnalyzeText(ctx context.Context, text string) (*ports.ExpenseAnalysis, error) {
	p := r.available()
	if p == nil {
		return nil, ports.ErrAIUnavailable
	}
	return p.AnalyzeText(ctx, text)
}

func (r *Router) AnalyzeImage(ctx context.Context, data []byte, mimeType string) (*ports.ExpenseAnalysis, error) {
	p := r.available()
	if p == nil {
		return nil, ports.ErrAIUnavailable
	}
	return p.AnalyzeImage(ctx, data, mimeType)
}

func (r *Router) AnalyzeDocument(ctx context.Context, data []byte, mimeType string) (*ports.StatementAnalysis, error) {
	p := r.available()
	if p == nil {
		return nil, ports.ErrAIUnavailable
	}
	return p.AnalyzeDocument(ctx, data, mimeType)
}

func (r *Router) Advise(ctx context.Context, data []byte) (ports.CoachAdvice, error) {
	p := r.available()
	if p == nil {
		return ports.CoachAdvice{}, ports.ErrAIUnavailable
	}
	return p.Advise(ctx, data)
}

func (r *Router) Categorize(ctx context.Context, data []byte) ([]ports.CategorySuggestion, error) {
	p := r.available()
	if p == nil {
		return nil, ports.ErrAIUnavailable
	}
	return p.Categorize(ctx, data)
}

func (r *Router) Close() error {
	r.mu.RLock()
	providers := make([]Provider, 0, len(r.providers))
	for _, p := range r.providers {
		providers = append(providers, p)
	}
	r.mu.RUnlock()
	var err error
	for _, p := range providers {
		err = errors.Join(err, p.Close())
	}
	return err
}

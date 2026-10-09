// Package app monta o aplicativo: abre o banco, lê as configurações, cria os serviços e liga canal, jobs e servidor HTTP.
// É o único lugar que conhece todas as peças; as regras de negócio ficam em usecase e as integrações em infra.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/MarcosAAlbanoJunior/go-financial-assistant/internal/usecase/insights"

	"github.com/MarcosAAlbanoJunior/go-financial-assistant/internal/usecase/ledger"

	"github.com/MarcosAAlbanoJunior/go-financial-assistant/internal/usecase/openfinance"

	"github.com/MarcosAAlbanoJunior/go-financial-assistant/internal/config"
	"github.com/MarcosAAlbanoJunior/go-financial-assistant/internal/domain/ports"
	"github.com/MarcosAAlbanoJunior/go-financial-assistant/internal/infra/ai"
	"github.com/MarcosAAlbanoJunior/go-financial-assistant/internal/infra/db"
	"github.com/MarcosAAlbanoJunior/go-financial-assistant/internal/infra/gemini"
	httpserver "github.com/MarcosAAlbanoJunior/go-financial-assistant/internal/infra/http"
	openaiinfra "github.com/MarcosAAlbanoJunior/go-financial-assistant/internal/infra/openai"
	"github.com/MarcosAAlbanoJunior/go-financial-assistant/internal/settings"
	"github.com/MarcosAAlbanoJunior/go-financial-assistant/internal/setup"
)

// app reúne as peças compartilhadas entre as etapas de montagem.
type app struct {
	ctx    context.Context
	cancel context.CancelFunc // encerra o app (usado pelo botão "Reiniciar" das configurações)
	logger *slog.Logger
	cfg    *config.Config

	db       *db.DB
	settings *settings.Service
	store    *db.SettingsStore
	onChange func() <-chan struct{} // devolve um canal que acorda quem lê a configuração quando ela muda

	gemini         *gemini.Client
	openai         *openaiinfra.Client
	aiRouter       *ai.Router
	ledger         ports.PurchaseRepository
	analyzeExpense *ledger.AnalyzeExpense
	exportCSV      *ledger.ExportCSV
	insights       *insights.Insights
	syncer         *openfinance.SyncOpenFinance
	server         *httpserver.Server
	settingsDeps   *httpserver.SettingsDeps
	secondFactor   *httpserver.SecondFactor
	setup          *setup.Service

	channel           channelSwitch // por onde o app fala com o dono; liga sem reiniciar
	channelConfigured atomic.Bool   // há canal configurado (ambiente ou setup), no ar ou não
	channelStarted    atomic.Bool   // o canal já foi ligado neste processo (no boot ou pelo setup)
}

// Run monta e executa o app até o contexto ser cancelado. Devolve erro só para falhas de inicialização ou do servidor.
func Run(ctx context.Context, cancel context.CancelFunc, logger *slog.Logger) error {
	a := &app{ctx: ctx, cancel: cancel, logger: logger}
	defer a.close()

	steps := []struct {
		name string
		do   func() error
	}{
		{"configuração", a.loadConfig},
		{"serviços", a.buildServices},
		{"sincronização", a.startSync},
		{"dashboard", a.mountDashboard},
	}
	for _, s := range steps {
		if err := s.do(); err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
	}

	err := a.startChannel()
	if errors.Is(err, context.Canceled) {
		return nil // encerrado enquanto esperava o WhatsApp conectar
	}
	if err != nil {
		return fmt.Errorf("canal: %w", err)
	}
	a.startJobs()

	logger.Info("starting go-financial-assistant", "port", a.cfg.Port)
	if err := a.server.Start(ctx); err != nil {
		return fmt.Errorf("servidor: %w", err)
	}
	logger.Info("server stopped gracefully")
	return nil
}

// buildServices cria os clientes externos, os repositórios e os casos de uso.
func (a *app) buildServices() error {
	var err error
	if a.gemini, err = gemini.NewClient(a.ctx, a.settings.Get("GEMINI_API_KEY")); err != nil {
		return fmt.Errorf("cliente do Gemini: %w", err)
	}
	a.openai = openaiinfra.NewClient(a.settings.Get("OPENAI_API_KEY"), a.settings.Get("OPENAI_MODEL"))
	a.aiRouter = ai.NewRouter(a.settings.Get("AI_PROVIDER"), a.gemini, a.openai)
	a.gemini.SetCoachModel(a.settings.Get("COACH_GEMINI_MODEL"))
	// O provedor, as chaves e os modelos valem sem reiniciar.
	a.settings.OnChange(func() {
		a.gemini.SetCoachModel(a.settings.Get("COACH_GEMINI_MODEL"))
		if err := a.gemini.SetAPIKey(a.ctx, a.settings.Get("GEMINI_API_KEY")); err != nil {
			a.logger.Error("chave do Gemini não aplicada", "error", err)
		}
		a.openai.SetAPIKey(a.settings.Get("OPENAI_API_KEY"))
		a.openai.SetModel(a.settings.Get("OPENAI_MODEL"))
		a.aiRouter.SetProvider(a.settings.Get("AI_PROVIDER"))
	})
	if !a.aiRouter.Ready() {
		a.logger.Warn("IA desligada: configure a chave do provedor ativo em Configurações → Inteligência Artificial", "provider", a.aiRouter.ProviderName())
	}

	a.ledger = db.NewPurchaseRepository(a.db)
	a.analyzeExpense = ledger.NewAnalyzeExpense(a.ledger, a.aiRouter, a.logger)
	a.exportCSV = ledger.NewExportCSV(a.ledger)
	a.insights = insights.NewInsights(db.NewDashboardReader(a.db))

	if err := a.analyzeExpense.GenerateRecurringExpenses(a.ctx); err != nil {
		a.logger.Error("erro ao gerar despesas recorrentes no startup", "error", err)
	}
	return nil
}

// applyTimezone adota o fuso configurado: a imagem roda em UTC, então sem isto horários e vencimentos saem 3 h adiantados.
func (a *app) applyTimezone() {
	if a.cfg.DigestLocation != nil {
		time.Local = a.cfg.DigestLocation
	}
}

// close libera o que já tinha sido aberto, mesmo se a montagem falhou no meio.
func (a *app) close() {
	if a.aiRouter != nil {
		_ = a.aiRouter.Close()
	}
	if a.db != nil {
		a.db.Close()
	}
}

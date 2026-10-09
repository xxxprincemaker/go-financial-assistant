package openai

import "github.com/MarcosAAlbanoJunior/go-financial-assistant/internal/infra/gemini"

func object(properties map[string]any, required ...string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func nullable(schema map[string]any) map[string]any {
	return map[string]any{"anyOf": []any{schema, map[string]any{"type": "null"}}}
}

func array(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}

func stringEnum(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}

func expenseSchema() map[string]any {
	query := object(map[string]any{
		"month": nullable(map[string]any{"type": "integer"}),
		"year":  nullable(map[string]any{"type": "integer"}),
	}, "month", "year")
	installments := object(map[string]any{
		"total":                  map[string]any{"type": "integer"},
		"amount_per_installment": map[string]any{"type": "number"},
	}, "total", "amount_per_installment")
	recurring := object(map[string]any{"day_of_month": map[string]any{"type": "integer"}}, "day_of_month")
	cancel := object(map[string]any{"description": map[string]any{"type": "string"}}, "description")
	return object(map[string]any{
		"type":               stringEnum("SINGLE", "INSTALLMENT", "RECURRING", "CANCEL_RECURRING", "INCOME", "INCOME_RECURRING", "TRANSFER", "QUERY", "EXPORT_CSV"),
		"amount":             nullable(map[string]any{"type": "number"}),
		"description":        nullable(map[string]any{"type": "string"}),
		"category":           nullable(map[string]any{"type": "string"}),
		"payment_method":     nullable(map[string]any{"type": "string"}),
		"transfer_direction": nullable(stringEnum("IN", "OUT")),
		"confidence":         map[string]any{"type": "number"},
		"installments":       nullable(installments),
		"recurring":          nullable(recurring),
		"cancel_recurring":   nullable(cancel),
		"query":              nullable(query),
		"export":             nullable(query),
	}, "type", "amount", "description", "category", "payment_method", "transfer_direction", "confidence", "installments", "recurring", "cancel_recurring", "query", "export")
}

func statementSchema() map[string]any {
	transaction := object(map[string]any{
		"date":            map[string]any{"type": "string"},
		"raw_description": map[string]any{"type": "string"},
		"description":     map[string]any{"type": "string"},
		"amount":          map[string]any{"type": "number"},
		"kind":            stringEnum("EXPENSE", "INCOME", "TRANSFER"),
		"direction":       nullable(stringEnum("IN", "OUT")),
		"category":        stringEnum("FOOD", "TRANSPORT", "HEALTH", "ENTERTAINMENT", "SHOPPING", "MARKET", "HOUSING", "BILLS", "EDUCATION", "PEOPLE", "INVESTMENT", "SALARY", "OTHER"),
		"payment_method":  stringEnum("PIX", "CREDIT_CARD", "DEBIT_CARD", "CASH", "OTHER"),
	}, "date", "raw_description", "description", "amount", "kind", "direction", "category", "payment_method")
	return object(map[string]any{"transactions": array(transaction)}, "transactions")
}

func coachSchema() map[string]any {
	action := object(map[string]any{
		"sugestao_id": map[string]any{"type": "string"},
		"prioridade":  map[string]any{"type": "integer", "minimum": 1, "maximum": 5},
		"comentario":  map[string]any{"type": "string"},
		"pergunta":    map[string]any{"type": "string"},
	}, "sugestao_id", "prioridade", "comentario", "pergunta")
	goal := object(map[string]any{
		"meta_id":    map[string]any{"type": "string"},
		"comentario": map[string]any{"type": "string"},
	}, "meta_id", "comentario")
	return object(map[string]any{
		"resumo":    map[string]any{"type": "string"},
		"acoes":     array(action),
		"metas":     array(goal),
		"perguntas": array(map[string]any{"type": "string"}),
	}, "resumo", "acoes", "metas", "perguntas")
}

func categorySchema() map[string]any {
	suggestion := object(map[string]any{
		"id":        map[string]any{"type": "string"},
		"categoria": stringEnum(gemini.CategorizeEnum...),
	}, "id", "categoria")
	return object(map[string]any{"sugestoes": array(suggestion)}, "sugestoes")
}

package ruleengine

import (
	"log/slog"
)

// Engine is the orchestrator for feature flag evaluation.
type Engine struct {
	strategies map[string]Evaluator
	logger     *slog.Logger // Dedicated logger instance (DI)
}

// New creates a new Engine.
// It requires a logger instance to ensure observability without relying on global state.
// If logger is nil, it defaults to slog.Default().
func New(logger *slog.Logger) *Engine {
	if logger == nil {
		logger = slog.Default()
	}

	return &Engine{
		logger: logger,
		strategies: map[string]Evaluator{
			RuleTypeUserIDList: &UserIDEvaluator{},
			RuleTypePercentage: &PercentageEvaluator{},
		},
	}
}

// Evaluate checks the input against a list of rules following priority order.
func (e *Engine) Evaluate(rules []Rule, input EvaluationInput) bool {
	for _, rule := range rules {
		strategy, exists := e.strategies[rule.Type]
		if !exists {
			e.logger.Warn("skipping unknown rule type",
				"type", rule.Type,
				"rule_id", rule.ID,
			)
			continue
		}

		match, err := strategy.Eval(rule.CompiledValue, input)
		if err != nil {
			// Fail Open Strategy: Log error and skip to next rule.
			// Do not crash the entire evaluation because of one bad rule.
			e.logger.Error("rule evaluation failed",
				"error", err,
				"rule_id", rule.ID,
				"type", rule.Type,
			)
			continue
		}

		if match {
			return true
		}
	}

	return false
}

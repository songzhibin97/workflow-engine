package rules

import (
	"fmt"
	"sync"

	"github.com/expr-lang/expr/vm"

	"github.com/expr-lang/expr"
)

// Evaluator defines the interface for evaluating rule expressions.
type Evaluator interface {
	Evaluate(expression string, context map[string]interface{}) (bool, error)
}

// ExprEvaluator is an implementation of Evaluator using expr-lang/expr.
type ExprEvaluator struct {
	cache       map[string]*vm.Program
	mu          sync.RWMutex
	optionsFunc map[string]func(map[string]interface{}) interface{}
}

// NewExprEvaluator creates a new ExprEvaluator with an initialized cache.
func NewExprEvaluator() *ExprEvaluator {
	return &ExprEvaluator{
		cache:       make(map[string]*vm.Program),
		optionsFunc: make(map[string]func(map[string]interface{}) interface{}),
	}
}

func (e *ExprEvaluator) AddOptionFunc(name string, f func(map[string]interface{}) interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.optionsFunc[name] = f
}

// Evaluate evaluates the given expression against the provided context.
// The expression must evaluate to a boolean; otherwise, an error is returned.
// Returns false and an error if compilation, execution, or type assertion fails.
func (e *ExprEvaluator) Evaluate(expression string, context map[string]interface{}) (bool, error) {
	for k, v := range e.optionsFunc {
		context[k] = v(context)
	}

	// Check cache with read lock
	e.mu.RLock()
	program, ok := e.cache[expression]
	e.mu.RUnlock()

	if !ok {
		// Compile with write lock
		e.mu.Lock()
		if program, ok = e.cache[expression]; !ok {
			var err error
			program, err = expr.Compile(expression, expr.Env(context))
			if err != nil {
				e.mu.Unlock()
				return false, err
			}
			e.cache[expression] = program
		}
		e.mu.Unlock()
	}

	result, err := expr.Run(program, context)
	if err != nil {
		return false, err
	}

	if boolResult, ok := result.(bool); ok {
		return boolResult, nil
	}
	return false, fmt.Errorf("expression '%s' did not evaluate to a boolean, got %T", expression, result)
}
